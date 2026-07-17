package server

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

// --- cache: server-to-server pending cache-update work item endpoints -----
// tech-docs/CACHE-UPDATES.md's pull model: a SHARD_READ_MEMBER or
// PROXY_READ_MEMBER lists, fetches, and deletes its own pending
// cache-update work items from a SHARD_SOT_MEMBER. Mirrors the shape of
// the pending-document-write-work-items endpoints in http.go.

type pendingCacheUpdateListRequest struct {
	ServerName string `json:"serverName"`
	Limit      int    `json:"limit"`
}

func (s *HTTPServer) handleListPendingCacheUpdates(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "Content-Type must be application/json",
		}))
		return
	}
	body, ferr := readBodyLimited(w, r, maxAuthRequestBodyBytes)
	if ferr != nil {
		writeJSON(w, envelope.Fail(nil, *ferr))
		return
	}
	var req pendingCacheUpdateListRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
		return
	}
	if req.ServerName == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Path:    "/serverName",
			Message: "serverName is required",
		}))
		return
	}
	if err := auth.RequireMachine(claims, req.ServerName); err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"serverName": req.ServerName}, toEnvelopeError(err)))
		return
	}
	ids, total, err := cache.ListWorkItemIDs(s.Engine.DataDir, req.ServerName, req.Limit)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"serverName": req.ServerName}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{
		"totalItems": total,
		"items":      ids,
	}))
}

func resolveCacheWorkItemDocID(claims auth.Claims, itemID string) (docID string, aerr *envelope.Error) {
	if err := auth.RequireMachine(claims, ""); err != nil {
		e := toEnvelopeError(err)
		return "", &e
	}
	docID, ok := cache.DocIDFromWorkItemID(itemID, claims.ServerName)
	if !ok {
		return "", &envelope.Error{
			Code:    "workItemNotFound",
			Message: "work item ID does not belong to the authenticated server",
		}
	}
	return docID, nil
}

func (s *HTTPServer) handleFetchPendingCacheUpdate(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, aerr := resolveCacheWorkItemDocID(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	_, item, err := cache.FindWorkItem(s.Engine.DataDir, claims.ServerName, docID)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
				Code:    "workItemNotFound",
				Message: "no pending work item exists for that ID",
			}))
			return
		}
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"item": item}))
}

func (s *HTTPServer) handleCompletePendingCacheUpdate(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, aerr := resolveCacheWorkItemDocID(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	sourceCollection, _, err := cache.FindWorkItem(s.Engine.DataDir, claims.ServerName, docID)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, envelope.OK(map[string]any{"completed": true, "existing": false}))
			return
		}
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	if _, err := cache.DeleteWorkItem(s.Engine.DataDir, sourceCollection, claims.ServerName, docID); err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"completed": true}))
}
