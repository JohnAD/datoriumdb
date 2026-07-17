package server

import (
	"encoding/json"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/idgen"
	"github.com/JohnAD/datoriumdb/internal/search"
	"net/http"
)

// --- search: server-to-server search result delivery -----------------------
// tech-docs/SERVER-TO-SERVER-API.md's search-result apply endpoint,
// implemented per internal/agents/change/remote.go's documented wire
// shape (a self-contained upsert/remove operation, resolved locally
// against this server's own establishment config, rather than the
// illustrative positional-RFC6902 shape SERVER-TO-SERVER-API.md leaves as
// an open detail).

type applySearchResultWriteRequest struct {
	TargetServer string                     `json:"targetServer"`
	Item         applySearchResultWriteItem `json:"item"`
}

type applySearchResultWriteItem struct {
	Collection string   `json:"collection"`
	Search     string   `json:"search"`
	Segments   []string `json:"segments"`
	Operation  string   `json:"operation"`
	ID         string   `json:"id"`
	Sort       []any    `json:"sort,omitempty"`
}

func (s *HTTPServer) handleApplySearchResultWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "Content-Type must be application/json",
		}))
		return
	}
	body, ferr := readBodyLimited(w, r, maxCommandBodyBytes)
	if ferr != nil {
		writeJSON(w, envelope.Fail(nil, *ferr))
		return
	}
	var req applySearchResultWriteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
		return
	}
	if req.TargetServer == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Path:    "/targetServer",
			Message: "targetServer is required",
		}))
		return
	}
	// Mirrors handleApplyDocumentWrite's push semantics: the caller here
	// is the pushing server (either the document's SOT routing to a
	// remote search-shard SOT, or the search-shard SOT replicating to a
	// read/proxy member), authenticating as itself, not as targetServer
	// (targetServer names the receiving server this delivery is meant
	// for, i.e. this server).
	if err := auth.RequireMachine(claims, ""); err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"targetServer": req.TargetServer}, toEnvelopeError(err)))
		return
	}
	if req.TargetServer != s.Engine.ServerName {
		writeJSON(w, envelope.Fail(map[string]any{"targetServer": req.TargetServer}, envelope.Error{
			Code:     "targetServerMismatch",
			Message:  "this delivery is not addressed to this server",
			Expected: s.Engine.ServerName,
			Actual:   req.TargetServer,
		}))
		return
	}
	item := req.Item
	if s.Engine.Cfg == nil {
		if err := s.Engine.Reload(); err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "configError", Message: err.Error()}))
			return
		}
	}
	defsForCollection, ok := s.Engine.Cfg.Searches[item.Collection]
	if !ok {
		writeJSON(w, envelope.Fail(map[string]any{"collection": item.Collection}, envelope.Error{
			Code:    "collectionNotFound",
			Message: "collection does not exist",
		}))
		return
	}
	raw, ok := defsForCollection[item.Search]
	if !ok {
		writeJSON(w, envelope.Fail(map[string]any{"collection": item.Collection, "search": item.Search}, envelope.Error{
			Code:    "searchNotFound",
			Message: "search definition does not exist",
		}))
		return
	}
	def, err := search.ParseDefinition(raw)
	if err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidSearchDefinition", Message: err.Error()}))
		return
	}
	path := fsstore.SearchResultPath(s.Engine.DataDir, item.Collection, item.Search, item.Segments)
	switch item.Operation {
	case "upsert":
		sortVals := search.SortValuesFromJSON(item.Sort)
		_, _, err = search.ApplyMutation(path, idgen.New, func(rf *search.ResultFile, existed bool) (bool, error) {
			rf.Search = item.Search
			rf.Collection = item.Collection
			return rf.Upsert(def, item.ID, sortVals), nil
		})
	case "remove":
		_, _, err = search.ApplyMutation(path, idgen.New, func(rf *search.ResultFile, existed bool) (bool, error) {
			if !existed {
				return false, nil
			}
			return rf.Remove(item.ID), nil
		})
	default:
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Path:    "/item/operation",
			Message: fmt.Sprintf("unknown operation %q", item.Operation),
		}))
		return
	}
	if err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "applyFailed", Message: err.Error()}))
		return
	}
	// applied is always true once err == nil: this endpoint's mutation is
	// idempotent by construction (search.ApplyMutation / ResultFile.Upsert
	// / ResultFile.Remove), whether or not this specific call changed the
	// file, matching how handleApplyDocumentWrite reports success.
	writeJSON(w, envelope.OK(map[string]any{"applied": true}))
}
