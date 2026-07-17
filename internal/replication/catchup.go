package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/shard"
)

// DefaultCheckinLimit is the default max number of work item references
// requested per check-in (SERVER-TO-SERVER-API.md's "limit" field).
const DefaultCheckinLimit = 200

// CatchUpAgent implements the read-member side of
// REPLICATION-FAILURE-HANDLING.md's "Read-Member Catch-Up": check in with
// relevant SOT-members for pending writes, apply them idempotently, and
// delete them through the SOT-member's API after durable success.
type CatchUpAgent struct {
	ServerName string
	DataDir    string
	Cfg        *config.Config
	Tokens     TokenSource
	State      *ReadMemberState

	HTTPClient *http.Client
	Limit      int
}

func (a *CatchUpAgent) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *CatchUpAgent) limit() int {
	if a.Limit > 0 {
		return a.Limit
	}
	return DefaultCheckinLimit
}

func (a *CatchUpAgent) baseURL(server string) string {
	if a.Cfg == nil {
		return ""
	}
	if entry, ok := a.Cfg.Servers.Servers[server]; ok {
		return entry.BaseURL
	}
	return ""
}

// RelevantSOTServers returns the distinct SHARD_SOT_MEMBER servers that
// serverName must check in with, because serverName is a SHARD_READ_MEMBER
// or PROXY_READ_MEMBER for at least one of that SOT-member's shard slots.
// REPLICATION-FAILURE-HANDLING.md treats both roles identically for
// replication.
func RelevantSOTServers(cfg *config.Config, serverName string) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, assignment := range cfg.ShardMap.ShardMap.Default {
		if assignment.ShardSOTMember == "" || assignment.ShardSOTMember == serverName {
			continue
		}
		if containsString(assignment.ShardReadMember, serverName) || containsString(assignment.ProxyReadMember, serverName) {
			if !seen[assignment.ShardSOTMember] {
				seen[assignment.ShardSOTMember] = true
				out = append(out, assignment.ShardSOTMember)
			}
		}
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// AssignmentForSlot finds the shard assignment covering slot in cfg's
// default shard map, or the zero value if none matches.
func AssignmentForSlot(cfg *config.Config, slot byte) config.ShardAssignment {
	if cfg == nil {
		return config.ShardAssignment{}
	}
	for raw, assignment := range cfg.ShardMap.ShardMap.Default {
		r, err := shard.ParseRange(raw)
		if err != nil {
			continue
		}
		if r.Contains(slot) {
			return assignment
		}
	}
	return config.ShardAssignment{}
}

// CheckIn performs one check-in cycle against sotServer: list pending work
// items targeted at this server, then fetch, apply, and complete each one
// in turn. It records the outcome on State so staleness thresholds and
// per-document refusal work correctly.
func (a *CatchUpAgent) CheckIn(ctx context.Context, sotServer string) error {
	base := a.baseURL(sotServer)
	if base == "" {
		return fmt.Errorf("replication: no known baseURL for SOT server %q", sotServer)
	}
	ids, _, err := a.listWorkItems(ctx, base)
	if err != nil {
		if a.State != nil {
			a.State.RecordCheckinFailure(sotServer)
		}
		return err
	}
	if a.State != nil {
		a.State.RecordCheckinSuccess(sotServer)
	}
	var firstErr error
	for _, id := range ids {
		if err := a.applyOne(ctx, base, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *CatchUpAgent) applyOne(ctx context.Context, base, itemID string) error {
	item, err := a.fetchWorkItem(ctx, base, itemID)
	if err != nil {
		return err
	}
	if a.State != nil {
		a.State.MarkPending(item.Collection, item.ID)
		defer a.State.ClearPending(item.Collection, item.ID)
	}
	applier := &Applier{DataDir: a.DataDir}
	applied, err := applier.Apply(*item)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("replication: work item %s did not apply", itemID)
	}
	return a.completeWorkItem(ctx, base, itemID)
}

func (a *CatchUpAgent) token(ctx context.Context) (string, error) {
	if a.Tokens == nil {
		return "", fmt.Errorf("replication: no token source configured")
	}
	return a.Tokens.Token(ctx)
}

func (a *CatchUpAgent) listWorkItems(ctx context.Context, base string) (ids []string, total int, err error) {
	tok, err := a.token(ctx)
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(map[string]any{"serverName": a.ServerName, "limit": a.limit()})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/datoriumdb/v1/sys/pending-document-write-work-items", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out struct {
		OK         bool     `json:"ok"`
		TotalItems int      `json:"totalItems"`
		Items      []string `json:"items"`
		Errors     []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	if !out.OK {
		return nil, 0, fmt.Errorf("replication: list pending work items failed: %s", firstMessage(out.Errors))
	}
	return out.Items, out.TotalItems, nil
}

func (a *CatchUpAgent) fetchWorkItem(ctx context.Context, base, itemID string) (*DocumentWorkItem, error) {
	tok, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/datoriumdb/v1/sys/pending-document-write-work-items/"+itemID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool             `json:"ok"`
		Item   DocumentWorkItem `json:"item"`
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("replication: fetch work item %s failed: %s", itemID, firstMessage(out.Errors))
	}
	return &out.Item, nil
}

func (a *CatchUpAgent) completeWorkItem(ctx context.Context, base, itemID string) error {
	tok, err := a.token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/datoriumdb/v1/sys/pending-document-write-work-items/"+itemID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		OK        bool `json:"ok"`
		Completed bool `json:"completed"`
		Errors    []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("replication: complete work item %s failed: %s", itemID, firstMessage(out.Errors))
	}
	return nil
}

func firstMessage(errs []struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}) string {
	if len(errs) == 0 {
		return "unknown error"
	}
	return fmt.Sprintf("%s: %s", errs[0].Code, errs[0].Message)
}
