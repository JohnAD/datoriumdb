package change

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/JohnAD/datoriumdb/internal/replication"
)

// RemoteApplier delivers a search-result bucket mutation to a remote
// search-shard SOT server over HTTP, per SERVER-TO-SERVER-API.md's "Happy-
// Path Search Result Delivery". SEARCHING.md notes search delivery may
// retry at the push layer instead of using a `.pendingWrites`-style
// fallback for MVP ("the SOT may retry push delivery and rely on the
// change-agent's retryable nature"); RemoteApplier returning an error
// leaves the change-agent's queue entry as `.taken` so the next scan
// retries the whole item, which satisfies that retry requirement.
//
// The request/response body used here is a simplified, self-contained
// operation description (collection/search/segments/operation/id/sort)
// rather than the illustrative positional-RFC6902 example shown in
// SERVER-TO-SERVER-API.md, because that document leaves the exact search
// patch wire shape as an open question ("Timeout fallback may use pending
// search-result work under the search directory in a later refinement").
// The receiving server re-resolves the search definition itself from its
// own establishment config and applies the same idempotent
// search.ResultFile.Upsert/Remove logic used locally.
type RemoteApplier struct {
	Cfg        ConfigSource
	Tokens     replication.TokenSource
	HTTPClient *http.Client
}

func (r *RemoteApplier) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return http.DefaultClient
}

type searchWriteRequest struct {
	TargetServer string          `json:"targetServer"`
	Item         searchWriteItem `json:"item"`
}

type searchWriteItem struct {
	Collection string   `json:"collection"`
	Search     string   `json:"search"`
	Segments   []string `json:"segments"`
	Operation  string   `json:"operation"` // "upsert" or "remove"
	ID         string   `json:"id"`
	Sort       []any    `json:"sort,omitempty"`
}

func (r *RemoteApplier) send(ctx context.Context, targetServer string, item searchWriteItem) error {
	baseURL := r.Cfg().ServerBaseURL(targetServer)
	if baseURL == "" {
		return fmt.Errorf("no known baseURL for server %q", targetServer)
	}
	tok, err := r.Tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("obtain machine token: %w", err)
	}
	body, err := json.Marshal(searchWriteRequest{TargetServer: targetServer, Item: item})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/datoriumdb/v1/sys/apply-search-result-write", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("call apply-search-result-write: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		OK      bool `json:"ok"`
		Applied bool `json:"applied"`
		Errors  []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode apply-search-result-write response: %w", err)
	}
	if !out.OK {
		msg := "unknown error"
		if len(out.Errors) > 0 {
			msg = fmt.Sprintf("%s: %s", out.Errors[0].Code, out.Errors[0].Message)
		}
		return fmt.Errorf("apply-search-result-write failed: %s", msg)
	}
	return nil
}

// Upsert delivers an upsert operation to targetServer.
func (r *RemoteApplier) Upsert(ctx context.Context, targetServer, collection, searchName string, segments []string, id string, sortJSON []any) error {
	return r.send(ctx, targetServer, searchWriteItem{
		Collection: collection,
		Search:     searchName,
		Segments:   segments,
		Operation:  "upsert",
		ID:         id,
		Sort:       sortJSON,
	})
}

// Remove delivers a remove operation to targetServer.
func (r *RemoteApplier) Remove(ctx context.Context, targetServer, collection, searchName string, segments []string, id string) error {
	return r.send(ctx, targetServer, searchWriteItem{
		Collection: collection,
		Search:     searchName,
		Segments:   segments,
		Operation:  "remove",
		ID:         id,
	})
}
