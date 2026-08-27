package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
)

// FileCatchUpAgent pulls pending binary writes from SOT members.
type FileCatchUpAgent struct {
	ServerName   string
	DataDir      string
	Cfg          *config.Config
	Tokens       TokenSource
	State        *ReadMemberState
	Timeout      time.Duration
	HTTPClient   *http.Client
	Limit        int
	MaxFileBytes int64
}

func (a *FileCatchUpAgent) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *FileCatchUpAgent) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return DefaultTimeout
}

func (a *FileCatchUpAgent) limit() int {
	if a.Limit > 0 {
		return a.Limit
	}
	return DefaultCheckinLimit
}

func (a *FileCatchUpAgent) baseURL(server string) string {
	if a.Cfg == nil {
		return ""
	}
	if entry, ok := a.Cfg.Servers.Servers[server]; ok {
		return entry.BaseURL
	}
	return ""
}

// CheckInFile pulls and applies pending file writes from sotServer.
func (a *FileCatchUpAgent) CheckInFile(ctx context.Context, sotServer string) error {
	base := a.baseURL(sotServer)
	if base == "" {
		return fmt.Errorf("no base URL for %s", sotServer)
	}
	ids, _, err := a.listWorkItems(ctx, base)
	if err != nil {
		return err
	}
	for _, itemID := range ids {
		if err := a.applyOne(ctx, base, itemID); err != nil {
			return err
		}
	}
	return nil
}

func (a *FileCatchUpAgent) applyOne(ctx context.Context, base, itemID string) error {
	item, err := a.fetchWorkItem(ctx, base, itemID)
	if err != nil {
		return err
	}
	if a.State != nil {
		a.State.MarkPendingFile(item.Collection, item.ID, item.Filename)
	}
	applier := &FileApplier{DataDir: a.DataDir}
	if item.Command == "fileDelete" {
		if _, err := applier.ApplyMetadataOnly(*item); err != nil {
			return err
		}
	} else {
		body, headers, err := a.fetchContent(ctx, base, itemID)
		if err != nil {
			return err
		}
		defer body.Close()
		if v := headers.Get("X-DatoriumDB-SHA256"); v != "" {
			item.SHA256 = v
		}
		if v := headers.Get("X-DatoriumDB-Byte-Size"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				item.ByteSize = n
			}
		}
		if ct := headers.Get("Content-Type"); ct != "" && item.ContentType == "" {
			item.ContentType = ct
		}
		if _, err := applier.ApplyStream(*item, body, a.MaxFileBytes); err != nil {
			return err
		}
	}
	if err := a.completeWorkItem(ctx, base, itemID); err != nil {
		return err
	}
	if a.State != nil {
		a.State.ClearPendingFile(item.Collection, item.ID, item.Filename)
	}
	return nil
}

func (a *FileCatchUpAgent) token(ctx context.Context) (string, error) {
	if a.Tokens == nil {
		return "", fmt.Errorf("no token source")
	}
	return a.Tokens.Token(ctx)
}

func (a *FileCatchUpAgent) listWorkItems(ctx context.Context, base string) (ids []string, total int, err error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	token, err := a.token(ctx)
	if err != nil {
		return nil, 0, err
	}
	payload, _ := json.Marshal(map[string]any{
		"serverName": a.ServerName,
		"limit":      a.limit(),
	})
	url := strings.TrimRight(base, "/") + "/datoriumdb/v1/sys/pending-file-write-work-items"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("list pending file writes: status %d: %s", resp.StatusCode, body)
	}
	var flat map[string]any
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, 0, err
	}
	if ok, _ := flat["ok"].(bool); !ok {
		return nil, 0, fmt.Errorf("list pending file writes not ok: %s", body)
	}
	if items, ok := flat["items"].([]any); ok {
		for _, it := range items {
			if s, ok := it.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	if t, ok := flat["totalItems"].(float64); ok {
		total = int(t)
	}
	return ids, total, nil
}

func (a *FileCatchUpAgent) fetchWorkItem(ctx context.Context, base, itemID string) (*FileWorkItem, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	token, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(base, "/") + "/datoriumdb/v1/sys/pending-file-write-work-items/" + urlPathEscape(itemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch pending file write: status %d: %s", resp.StatusCode, body)
	}
	var flat map[string]any
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, err
	}
	rawItem, err := json.Marshal(flat["item"])
	if err != nil {
		return nil, err
	}
	var item FileWorkItem
	if err := json.Unmarshal(rawItem, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (a *FileCatchUpAgent) fetchContent(ctx context.Context, base, itemID string) (io.ReadCloser, http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	token, err := a.token(ctx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	url := strings.TrimRight(base, "/") + "/datoriumdb/v1/sys/pending-file-write-work-items/" + urlPathEscape(itemID) + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		cancel()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, nil, fmt.Errorf("fetch pending file content: status %d: %s", resp.StatusCode, b)
	}
	return &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, resp.Header, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func (a *FileCatchUpAgent) completeWorkItem(ctx context.Context, base, itemID string) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	token, err := a.token(ctx)
	if err != nil {
		return err
	}
	url := strings.TrimRight(base, "/") + "/datoriumdb/v1/sys/pending-file-write-work-items/" + urlPathEscape(itemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("complete pending file write: status %d", resp.StatusCode)
	}
	return nil
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}
