package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/replication"
)

func (s *HTTPServer) handleApplyFileWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	if err := auth.RequireMachine(claims, ""); err != nil {
		writeJSON(w, envelope.Fail(nil, toEnvelopeError(err)))
		return
	}
	target := r.Header.Get("X-DatoriumDB-Target-Server")
	if target == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: "X-DatoriumDB-Target-Server is required"}))
		return
	}
	if target != s.Engine.ServerName {
		writeJSON(w, envelope.Fail(map[string]any{"targetServer": target}, envelope.Error{
			Code:     "targetServerMismatch",
			Message:  "this delivery is not addressed to this server",
			Expected: s.Engine.ServerName,
			Actual:   target,
		}))
		return
	}
	item := replication.FileWorkItem{
		Collection:  r.Header.Get("X-DatoriumDB-Collection"),
		ID:          r.Header.Get("X-DatoriumDB-Document-Id"),
		Filename:    r.Header.Get("X-DatoriumDB-Filename"),
		ContentType: r.Header.Get("Content-Type"),
		SHA256:      r.Header.Get("X-DatoriumDB-SHA256"),
		Version:     r.Header.Get("X-DatoriumDB-Version"),
		OperationID: r.Header.Get("X-DatoriumDB-Operation-Id"),
		Command:     r.Header.Get("X-DatoriumDB-Command"),
	}
	if raw := r.Header.Get("X-DatoriumDB-Byte-Size"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			item.ByteSize = n
		}
	}
	if item.Collection == "" || item.ID == "" || item.Filename == "" || item.Command == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: "file apply headers are incomplete"}))
		return
	}
	if !fsstore.SafeFileName(item.Filename) {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidFileName", Message: "file name is invalid"}))
		return
	}
	applier := &replication.FileApplier{DataDir: s.Engine.DataDir}
	var (
		applied bool
		err     error
	)
	maxBytes := fsstore.DefaultMaxFileBytes
	if s.Engine.Cfg != nil && s.Engine.Cfg.General.General.MaxFileBytes > 0 {
		maxBytes = s.Engine.Cfg.General.General.MaxFileBytes
	}
	if item.Command == "fileDelete" {
		applied, err = applier.ApplyMetadataOnly(item)
	} else {
		applied, err = applier.ApplyStream(item, r.Body, maxBytes)
	}
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"operationId": item.OperationID}, envelope.Error{
			Code:    "applyFailed",
			Message: err.Error(),
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{
		"applied":     applied,
		"operationId": item.OperationID,
	}))
}

func (s *HTTPServer) handleListPendingFileWrites(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
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
	var req pendingWriteListRequest
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
	ids, total, err := replication.ListPendingFileWorkItemIDs(s.Engine.DataDir, req.ServerName, req.Limit)
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

func (s *HTTPServer) handleFetchPendingFileWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, filename, aerr := resolveFileWorkItem(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	_, item, err := replication.FindPendingFileWrite(s.Engine.DataDir, claims.ServerName, docID, filename)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "workItemNotFound",
			Message: "pending file write not found",
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"item": item}))
}

func (s *HTTPServer) handleFetchPendingFileContent(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, filename, aerr := resolveFileWorkItem(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	_, item, err := replication.FindPendingFileWrite(s.Engine.DataDir, claims.ServerName, docID, filename)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "workItemNotFound",
			Message: "pending file write not found",
		}))
		return
	}
	if item.Command == "fileDelete" {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "invalidRequest",
			Message: "delete pending file writes have no content body",
		}))
		return
	}
	f, err := replication.OpenLocalFileBody(s.Engine.DataDir, *item)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	defer f.Close()
	ct := item.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(item.ByteSize, 10))
	w.Header().Set("X-DatoriumDB-SHA256", item.SHA256)
	w.Header().Set("X-DatoriumDB-Byte-Size", strconv.FormatInt(item.ByteSize, 10))
	w.Header().Set("X-DatoriumDB-Version", item.Version)
	w.Header().Set("X-DatoriumDB-Operation-Id", item.OperationID)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func (s *HTTPServer) handleCompletePendingFileWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, filename, aerr := resolveFileWorkItem(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	collection, _, err := replication.FindPendingFileWrite(s.Engine.DataDir, claims.ServerName, docID, filename)
	if err != nil {
		writeJSON(w, envelope.OK(map[string]any{"completed": true, "existing": false}))
		return
	}
	existed, err := replication.DeletePendingFileWrite(s.Engine.DataDir, collection, claims.ServerName, docID, filename)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	if !existed {
		writeJSON(w, envelope.OK(map[string]any{"completed": true, "existing": false}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"completed": true}))
}

func resolveFileWorkItem(claims auth.Claims, itemID string) (docID, filename string, aerr *envelope.Error) {
	if err := auth.RequireMachine(claims, ""); err != nil {
		e := toEnvelopeError(err)
		return "", "", &e
	}
	docID, filename, ok := replication.ParseFileWorkItemID(itemID, claims.ServerName)
	if !ok {
		return "", "", &envelope.Error{
			Code:    "workItemNotFound",
			Message: "work item ID does not belong to the authenticated server",
		}
	}
	return docID, filename, nil
}
