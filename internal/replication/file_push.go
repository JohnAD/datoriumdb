package replication

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// BuildFileNote builds the client-facing replication note for incomplete
// binary distribution (same shape as document notes).
func BuildFileNote(outcome PushOutcome) map[string]any {
	return map[string]any{
		"code":           "fileReplicationIncomplete",
		"message":        "One or more read/proxy members did not acknowledge the file write within the delivery timeout; the latest desired state remains pending for catch-up.",
		"required":       outcome.Required,
		"acknowledged":   outcome.Acknowledged,
		"unacknowledged": outcome.Unacknowledged,
		"timeoutMs":      outcome.TimeoutMs,
	}
}

// DeliverFileOnce is the SOT binary delivery contract: persist coalescing
// pending state before each push, push once, delete pending only on ack.
func (c *Coordinator) DeliverFileOnce(ctx context.Context, item FileWorkItem, targets []string) PushOutcome {
	outcome := PushOutcome{
		Required:  append([]string{}, targets...),
		TimeoutMs: int(c.timeout() / time.Millisecond),
	}
	if len(targets) == 0 {
		return outcome
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			// Persist latest desired state before attempting push so a crash
			// mid-push still leaves catch-up work.
			_ = WritePendingFileWrite(c.DataDir, item.Collection, target, item)
			ok := c.pushFileOne(ctx, target, item)
			mu.Lock()
			defer mu.Unlock()
			if ok {
				outcome.Acknowledged = append(outcome.Acknowledged, target)
				_, _ = DeletePendingFileWrite(c.DataDir, item.Collection, target, item.ID, item.Filename)
			} else {
				outcome.Unacknowledged = append(outcome.Unacknowledged, target)
			}
		}(target)
	}
	wg.Wait()
	return outcome
}

func (c *Coordinator) pushFileOne(ctx context.Context, target string, item FileWorkItem) bool {
	base := c.baseURL(target)
	if base == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return false
	}
	url := stringsTrimRightSlash(base) + "/datoriumdb/v1/sys/apply-file-write"
	var body io.Reader
	var contentLength int64
	if item.Command != "fileDelete" {
		f, err := fsstore.OpenBinary(c.DataDir, item.Collection, item.ID, item.Filename)
		if err != nil {
			return false
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return false
		}
		body = f
		contentLength = st.Size()
	} else {
		body = http.NoBody
		contentLength = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-DatoriumDB-Target-Server", target)
	req.Header.Set("X-DatoriumDB-Collection", item.Collection)
	req.Header.Set("X-DatoriumDB-Document-Id", item.ID)
	req.Header.Set("X-DatoriumDB-Filename", item.Filename)
	req.Header.Set("X-DatoriumDB-Command", item.Command)
	req.Header.Set("X-DatoriumDB-Version", item.Version)
	req.Header.Set("X-DatoriumDB-Operation-Id", item.OperationID)
	if item.Command != "fileDelete" {
		req.Header.Set("Content-Type", item.ContentType)
		if item.ContentType == "" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		req.Header.Set("X-DatoriumDB-SHA256", item.SHA256)
		req.Header.Set("X-DatoriumDB-Byte-Size", strconv.FormatInt(item.ByteSize, 10))
		req.ContentLength = contentLength
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = 0
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func stringsTrimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// OpenLocalFileBody opens the local LFS bytes for catch-up fetch of a pending item.
func OpenLocalFileBody(dataDir string, item FileWorkItem) (*os.File, error) {
	if item.Command == "fileDelete" {
		return nil, fmt.Errorf("delete has no body")
	}
	return fsstore.OpenBinary(dataDir, item.Collection, item.ID, item.Filename)
}
