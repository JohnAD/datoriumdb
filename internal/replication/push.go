package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
)

// DefaultTimeout is REPLICATION-FAILURE-HANDLING.md's "reasonable starting
// timeout" for happy-path push delivery before falling back to
// .pendingWrites.
const DefaultTimeout = 10 * time.Second

// TokenSource returns a bearer token this server can present to another
// DatoriumDB server's /sys endpoints, per AUTHENTICATION.md's machine token
// model.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// PushOutcome describes what happened when an SOT-member tried to deliver
// one document write to every required read/proxy member, for building the
// REPLICATION-FAILURE-HANDLING.md "Client Response Shape" note.
type PushOutcome struct {
	Required       []string `json:"required"`
	Acknowledged   []string `json:"acknowledged"`
	Unacknowledged []string `json:"unacknowledged"`
	TimeoutMs      int      `json:"timeoutMs"`
}

// Complete reports whether every required target acknowledged.
func (o PushOutcome) Complete() bool {
	return len(o.Unacknowledged) == 0
}

// Coordinator pushes document writes from an SOT-member to its shard
// slot's read/proxy members and records durable pending writes for any
// target that did not acknowledge within Timeout.
type Coordinator struct {
	ServerName string
	DataDir    string
	Cfg        *config.Config
	Tokens     TokenSource

	// Timeout bounds each push attempt; defaults to DefaultTimeout (10s).
	Timeout time.Duration
	// HTTPClient defaults to http.DefaultClient's transport with no
	// additional timeout beyond the per-request context deadline.
	HTTPClient *http.Client
}

func (c *Coordinator) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

func (c *Coordinator) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Coordinator) baseURL(server string) string {
	if c.Cfg == nil {
		return ""
	}
	if entry, ok := c.Cfg.Servers.Servers[server]; ok {
		return entry.BaseURL
	}
	return ""
}

// TargetsForAssignment returns the read+proxy members that must receive a
// replicated write for a shard slot's assignment, excluding this server
// itself. REPLICATION-FAILURE-HANDLING.md treats SHARD_READ_MEMBER and
// PROXY_READ_MEMBER identically for replication.
func (c *Coordinator) TargetsForAssignment(assignment config.ShardAssignment) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || name == c.ServerName || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, n := range assignment.ShardReadMember {
		add(n)
	}
	for _, n := range assignment.ProxyReadMember {
		add(n)
	}
	return out
}

// ReplicateDocumentWrite pushes item to every target in parallel, waiting
// up to Timeout for each. Any target that does not acknowledge gets a
// durable .pendingWrites entry (REPLICATION-FAILURE-HANDLING.md's
// "Pending Writes Layout") so a later read-member catch-up can finish the
// job.
func (c *Coordinator) ReplicateDocumentWrite(ctx context.Context, item DocumentWorkItem, targets []string) PushOutcome {
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
			ok := c.pushOne(ctx, target, item)
			mu.Lock()
			defer mu.Unlock()
			if ok {
				outcome.Acknowledged = append(outcome.Acknowledged, target)
				return
			}
			outcome.Unacknowledged = append(outcome.Unacknowledged, target)
			_ = WritePendingWrite(c.DataDir, item.Collection, target, item)
		}(target)
	}
	wg.Wait()
	return outcome
}

func (c *Coordinator) pushOne(ctx context.Context, target string, item DocumentWorkItem) bool {
	base := c.baseURL(target)
	if base == "" {
		return false
	}
	pushCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	tok, err := c.Tokens.Token(pushCtx)
	if err != nil {
		return false
	}
	body, err := json.Marshal(map[string]any{"targetServer": target, "item": item})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(pushCtx, http.MethodPost, base+"/datoriumdb/v1/sys/apply-document-write", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var out struct {
		OK      bool `json:"ok"`
		Applied bool `json:"applied"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false
	}
	return out.OK && out.Applied
}

// NoteCode is the stable note.code used in REPLICATION-FAILURE-HANDLING.md's
// "Client Response Shape".
const NoteCode = "replication_retry_scheduled"

// BuildNote builds the response "note" object described in
// REPLICATION-FAILURE-HANDLING.md when one or more required read/proxy
// members did not acknowledge a write within the timeout.
func BuildNote(outcome PushOutcome) map[string]any {
	return map[string]any{
		"code":           NoteCode,
		"message":        "Write succeeded on the SOT-member, but one or more read members did not acknowledge within the timeout. Pending write work has been scheduled.",
		"required":       nonNilStrings(outcome.Required),
		"acknowledged":   nonNilStrings(outcome.Acknowledged),
		"unacknowledged": nonNilStrings(outcome.Unacknowledged),
		"timeoutMs":      outcome.TimeoutMs,
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// ErrNoBaseURL is returned when a target server has no known baseURL.
var ErrNoBaseURL = fmt.Errorf("replication: target server has no known baseURL")
