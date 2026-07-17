package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/replication"
)

// DefaultCheckinLimit is the default max number of work item references
// requested per check-in.
const DefaultCheckinLimit = 200

// Agent implements the read-member side of CACHE-UPDATES.md's pull model:
// periodically check in with every SHARD_SOT_MEMBER other than itself,
// fetch pending cache-update work items targeted at this server, apply
// them locally, and delete them from the SOT-member once durably applied.
type Agent struct {
	ServerName string
	DataDir    string
	Cfg        func() *config.Config
	Tokens     replication.TokenSource

	HTTPClient *http.Client
	Limit      int
	Logf       func(format string, args ...any)
}

func (a *Agent) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *Agent) limit() int {
	if a.Limit > 0 {
		return a.Limit
	}
	return DefaultCheckinLimit
}

func (a *Agent) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// RunOnce implements scheduler.Task: check in with every relevant
// SOT-member once, applying and completing any pending cache-update work
// items found. It reports didWork=true if any work item was applied, so
// the scheduler's drain loop keeps going while there is a backlog.
func (a *Agent) RunOnce(ctx context.Context) (bool, error) {
	cfg := a.Cfg()
	if cfg == nil {
		return false, nil
	}
	did := false
	var firstErr error
	for _, sot := range cfg.AllSOTMembers() {
		if sot == a.ServerName {
			continue
		}
		n, err := a.checkIn(ctx, cfg, sot)
		if n > 0 {
			did = true
		}
		if err != nil {
			a.logf("cache-agent: check-in with %s: %v", sot, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return did, firstErr
}

func (a *Agent) checkIn(ctx context.Context, cfg *config.Config, sotServer string) (applied int, err error) {
	base := cfg.ServerBaseURL(sotServer)
	if base == "" {
		return 0, fmt.Errorf("no known baseURL for server %q", sotServer)
	}
	ids, _, err := a.listWorkItems(ctx, base)
	if err != nil {
		return 0, err
	}
	var firstErr error
	count := 0
	for _, id := range ids {
		if err := a.applyOne(ctx, base, id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		count++
	}
	return count, firstErr
}

func (a *Agent) applyOne(ctx context.Context, base, itemID string) error {
	item, err := a.fetchWorkItem(ctx, base, itemID)
	if err != nil {
		return err
	}
	if _, err := Apply(a.DataDir, *item); err != nil {
		return err
	}
	return a.completeWorkItem(ctx, base, itemID)
}

func (a *Agent) token(ctx context.Context) (string, error) {
	if a.Tokens == nil {
		return "", fmt.Errorf("no token source configured")
	}
	return a.Tokens.Token(ctx)
}

type cacheErrList = []struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a *Agent) listWorkItems(ctx context.Context, base string) (ids []string, total int, err error) {
	tok, err := a.token(ctx)
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(map[string]any{"serverName": a.ServerName, "limit": a.limit()})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/datoriumdb/v1/sys/pending-cache-update-work-items", bytes.NewReader(body))
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
		OK         bool         `json:"ok"`
		TotalItems int          `json:"totalItems"`
		Items      []string     `json:"items"`
		Errors     cacheErrList `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	if !out.OK {
		return nil, 0, fmt.Errorf("list pending cache-update work items failed: %s", firstMessage(out.Errors))
	}
	return out.Items, out.TotalItems, nil
}

func (a *Agent) fetchWorkItem(ctx context.Context, base, itemID string) (*WorkItem, error) {
	tok, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/datoriumdb/v1/sys/pending-cache-update-work-items/"+itemID, nil)
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
		OK     bool         `json:"ok"`
		Item   WorkItem     `json:"item"`
		Errors cacheErrList `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("fetch cache-update work item %s failed: %s", itemID, firstMessage(out.Errors))
	}
	return &out.Item, nil
}

func (a *Agent) completeWorkItem(ctx context.Context, base, itemID string) error {
	tok, err := a.token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/datoriumdb/v1/sys/pending-cache-update-work-items/"+itemID, nil)
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
		OK        bool         `json:"ok"`
		Completed bool         `json:"completed"`
		Errors    cacheErrList `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("complete cache-update work item %s failed: %s", itemID, firstMessage(out.Errors))
	}
	return nil
}

func firstMessage(errs cacheErrList) string {
	if len(errs) == 0 {
		return "unknown error"
	}
	return fmt.Sprintf("%s: %s", errs[0].Code, errs[0].Message)
}
