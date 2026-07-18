// Package server exposes DatoriumDB's HTTP transport: establishment reads,
// access-language commands, machine-token issuance, historic schema
// retrieval, health/readiness probes, and a server-to-server /sys sample
// endpoint. See tech-docs/LOCAL-ARCHITECTURE.md, AUTHENTICATION.md, and
// ESTABLISHMENT-CONFIG.md.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/replication"
)

const (
	maxCommandBodyBytes     = 1 << 20 // 1 MiB access-language command body
	maxAuthRequestBodyBytes = 16 << 10
)

// HTTPServer serves the DatoriumDB HTTP API described in
// tech-docs/LOCAL-ARCHITECTURE.md and tech-docs/AUTHENTICATION.md.
type HTTPServer struct {
	Engine *engine.Engine

	// Issuer signs client and machine tokens. Only set on the establishment
	// server when DATORIUMDB_SIGNING_KEY_FILE is configured; nil elsewhere.
	Issuer *auth.Issuer

	// BootstrapSecret is the shared cluster secret from
	// DATORIUMDB_MACHINE_BOOTSTRAP_SECRET, checked by the machine-token
	// endpoint's initial-bootstrap path.
	BootstrapSecret string
}

func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /datoriumdb/v1/health", s.handleHealth)
	mux.HandleFunc("GET /datoriumdb/v1/ready", s.handleReady)
	mux.HandleFunc("POST /datoriumdb/v1/auth/machine-token", s.handleMachineToken)
	mux.HandleFunc("GET /datoriumdb/v1/establish", s.withAuth(s.handleEstablish))
	mux.HandleFunc("POST /datoriumdb/v1/command", s.withAuth(s.handleCommand))
	mux.HandleFunc("GET /datoriumdb/v1/schema/{collection}/{ver}", s.withAuth(s.handleSchemaHistory))
	mux.HandleFunc("GET /datoriumdb/v1/sys/ping/{serverName}", s.withAuth(s.handleSysPing))
	mux.HandleFunc("POST /datoriumdb/v1/sys/apply-document-write", s.withAuth(s.handleApplyDocumentWrite))
	mux.HandleFunc("POST /datoriumdb/v1/sys/pending-document-write-work-items", s.withAuth(s.handleListPendingDocumentWrites))
	mux.HandleFunc("GET /datoriumdb/v1/sys/pending-document-write-work-items/{itemID}", s.withAuth(s.handleFetchPendingDocumentWrite))
	mux.HandleFunc("DELETE /datoriumdb/v1/sys/pending-document-write-work-items/{itemID}", s.withAuth(s.handleCompletePendingDocumentWrite))
	mux.HandleFunc("POST /datoriumdb/v1/sys/apply-search-result-write", s.withAuth(s.handleApplySearchResultWrite))
	mux.HandleFunc("POST /datoriumdb/v1/sys/pending-cache-update-work-items", s.withAuth(s.handleListPendingCacheUpdates))
	mux.HandleFunc("GET /datoriumdb/v1/sys/pending-cache-update-work-items/{itemID}", s.withAuth(s.handleFetchPendingCacheUpdate))
	mux.HandleFunc("DELETE /datoriumdb/v1/sys/pending-cache-update-work-items/{itemID}", s.withAuth(s.handleCompletePendingCacheUpdate))
	return mux
}

// --- liveness / readiness -------------------------------------------------

func (s *HTTPServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, envelope.OK(map[string]any{"alive": true}))
}

func (s *HTTPServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.Engine == nil || s.Engine.Cfg == nil {
		writeJSON(w, envelope.Fail(map[string]any{"ready": false}, envelope.Error{
			Code:    "notReady",
			Message: "establishment config is not loaded yet",
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{
		"ready":          true,
		"generalVersion": s.Engine.Cfg.General.General.Version,
	}))
}

// --- establishment / command / schema history -----------------------------

func (s *HTTPServer) handleEstablish(w http.ResponseWriter, _ *http.Request, _ auth.Claims) {
	if err := s.Engine.Reload(); err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "configError", Message: err.Error()}))
		return
	}
	fields := s.Engine.Cfg.EstablishDocument()
	writeJSON(w, envelope.OK(fields))
}

func (s *HTTPServer) handleCommand(w http.ResponseWriter, r *http.Request, _ auth.Claims) {
	if !isPlainTextUTF8(r.Header.Get("Content-Type")) {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "Content-Type must be text/plain; charset=utf-8",
		}))
		return
	}
	body, ferr := readBodyLimited(w, r, maxCommandBodyBytes)
	if ferr != nil {
		writeJSON(w, envelope.Fail(nil, *ferr))
		return
	}
	if s.Engine.Cfg == nil {
		if err := s.Engine.Reload(); err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "configError", Message: err.Error()}))
			return
		}
	}
	writeJSON(w, s.Engine.Execute(string(body)))
}

func (s *HTTPServer) handleSchemaHistory(w http.ResponseWriter, r *http.Request, _ auth.Claims) {
	collection := r.PathValue("collection")
	verRaw := r.PathValue("ver")
	ver, err := strconv.Atoi(verRaw)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"collection": collection}, envelope.Error{
			Code:    "invalidRequest",
			Path:    "/ver",
			Message: "version must be an integer",
			Actual:  verRaw,
		}))
		return
	}
	if s.Engine.Cfg == nil {
		if err := s.Engine.Reload(); err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "configError", Message: err.Error()}))
			return
		}
	}
	history, ok := s.Engine.Cfg.SchemaHistory[collection]
	if !ok {
		writeJSON(w, envelope.Fail(map[string]any{"collection": collection}, envelope.Error{
			Code:    "collectionNotFound",
			Message: "collection does not exist",
		}))
		return
	}
	raw, ok := history[ver]
	if !ok {
		writeJSON(w, envelope.Fail(map[string]any{"collection": collection, "version": ver}, envelope.Error{
			Code:    "schemaVersionNotFound",
			Message: "no historic schema found for that version",
		}))
		return
	}
	// Embed as RawMessage so field order is not destroyed by map[string]any.
	writeJSON(w, envelope.OK(map[string]any{
		"collection": collection,
		"version":    ver,
		"schema":     json.RawMessage(raw),
	}))
}

// --- server-to-server sample endpoint --------------------------------------

// handleSysPing is a minimal server-to-server endpoint demonstrating the
// /sys authorization rule from tech-docs/AUTHENTICATION.md and
// SERVER-TO-SERVER-API.md: callers must present a machine token whose
// datoriumdb.serverName matches the serverName whose work is requested.
func (s *HTTPServer) handleSysPing(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	serverName := r.PathValue("serverName")
	if err := auth.RequireMachine(claims, serverName); err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"serverName": serverName}, toEnvelopeError(err)))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"pong": true, "serverName": serverName}))
}

// --- replication: server-to-server document write endpoints ---------------
// tech-docs/SERVER-TO-SERVER-API.md's "Happy-Path Document Write Delivery"
// and "Pending Writes" sections.

type applyDocumentWriteRequest struct {
	TargetServer string                       `json:"targetServer"`
	Item         replication.DocumentWorkItem `json:"item"`
}

// handleApplyDocumentWrite is the read/proxy member side of the SOT's
// happy-path push: apply one document write idempotently to local
// shard-local storage.
func (s *HTTPServer) handleApplyDocumentWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
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
	var req applyDocumentWriteRequest
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
	// The caller here is the pushing SHARD_SOT_MEMBER, authenticating as
	// itself, not as targetServer (targetServer names the receiving
	// server this delivery is meant for, i.e. this server). Any
	// authenticated machine may push; a mismatched targetServer means the
	// SOT-member misrouted the delivery.
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
	applier := &replication.Applier{DataDir: s.Engine.DataDir}
	applied, err := applier.Apply(req.Item)
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{
			"operationId": req.Item.OperationID,
		}, envelope.Error{Code: "applyFailed", Message: err.Error()}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{
		"applied":     applied,
		"operationId": req.Item.OperationID,
	}))
}

type pendingWriteListRequest struct {
	ServerName string `json:"serverName"`
	Limit      int    `json:"limit"`
}

// handleListPendingDocumentWrites returns opaque work item ID references
// for pending document writes targeted at the authenticated read/proxy
// member. QUERY is carried as POST for the MVP HTTP implementation.
func (s *HTTPServer) handleListPendingDocumentWrites(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
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
	ids, total, err := replication.ListPendingWorkItemIDs(s.Engine.DataDir, req.ServerName, req.Limit)
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

// resolveWorkItemDocID authenticates the caller as a machine, then decodes
// itemID against the caller's own authenticated server name. Per
// SERVER-TO-SERVER-API.md: "The authenticated server identity must match
// the serverName whose work is being requested, fetched, or deleted."
// Because the work item's target-server component must match the
// authenticated identity, callers can never fetch or delete another
// server's pending work by guessing a differently-prefixed item ID.
func resolveWorkItemDocID(claims auth.Claims, itemID string) (docID string, aerr *envelope.Error) {
	if err := auth.RequireMachine(claims, ""); err != nil {
		e := toEnvelopeError(err)
		return "", &e
	}
	docID, ok := replication.DocIDFromWorkItemID(itemID, claims.ServerName)
	if !ok {
		return "", &envelope.Error{
			Code:    "workItemNotFound",
			Message: "work item ID does not belong to the authenticated server",
		}
	}
	return docID, nil
}

// handleFetchPendingDocumentWrite returns one pending document write work
// item's full body. Fetching does not delete it.
func (s *HTTPServer) handleFetchPendingDocumentWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, aerr := resolveWorkItemDocID(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	_, item, err := replication.FindPendingWrite(s.Engine.DataDir, claims.ServerName, docID)
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

// handleCompletePendingDocumentWrite confirms durable local application and
// asks the SOT-member to delete the stored work item. Deleting an
// already-absent item is not an error (existing:false).
func (s *HTTPServer) handleCompletePendingDocumentWrite(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	itemID := r.PathValue("itemID")
	docID, aerr := resolveWorkItemDocID(claims, itemID)
	if aerr != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, *aerr))
		return
	}
	collection, _, err := replication.FindPendingWrite(s.Engine.DataDir, claims.ServerName, docID)
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
	if _, err := replication.DeletePendingWrite(s.Engine.DataDir, collection, claims.ServerName, docID); err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"itemId": itemID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{"completed": true}))
}

// --- machine token bootstrap / renewal -------------------------------------

type machineTokenRequest struct {
	ServerName      string `json:"serverName"`
	BootstrapSecret string `json:"bootstrapSecret"`
}

func (s *HTTPServer) handleMachineToken(w http.ResponseWriter, r *http.Request) {
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
	var req machineTokenRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
			return
		}
	}
	if req.ServerName == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Path:    "/serverName",
			Message: "serverName is required",
		}))
		return
	}

	authHeader := r.Header.Get("Authorization")
	switch {
	case authHeader != "":
		// Renewal path: a not-yet-expired machine token stands in for the
		// bootstrap secret (tech-docs/AUTHENTICATION.md).
		validator, verr := s.currentValidator()
		if verr != nil {
			writeJSON(w, envelope.Fail(nil, *verr))
			return
		}
		claims, err := validator.ParseBearer(authHeader)
		if err != nil {
			writeJSON(w, envelope.Fail(nil, toEnvelopeError(err)))
			return
		}
		if merr := auth.RequireMachine(claims, req.ServerName); merr != nil {
			writeJSON(w, envelope.Fail(nil, toEnvelopeError(merr)))
			return
		}
	case req.BootstrapSecret != "":
		if s.BootstrapSecret == "" ||
			subtle.ConstantTimeCompare([]byte(req.BootstrapSecret), []byte(s.BootstrapSecret)) != 1 {
			writeJSON(w, envelope.Fail(nil, envelope.Error{
				Code:    "invalidBootstrapSecret",
				Message: "bootstrap secret is invalid",
			}))
			return
		}
	default:
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "unauthenticated",
			Message: "bootstrapSecret or an Authorization bearer token is required",
		}))
		return
	}

	if s.Issuer == nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "machineTokenIssuanceUnavailable",
			Message: "this server does not hold a signing key and cannot issue machine tokens",
		}))
		return
	}
	token, lifetime, err := s.Issuer.IssueMachineToken(req.ServerName, 0)
	if err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "tokenIssuanceFailed", Message: err.Error()}))
		return
	}
	writeJSON(w, envelope.OK(map[string]any{
		"token":     token,
		"expiresIn": int(lifetime.Seconds()),
	}))
}

// --- auth middleware --------------------------------------------------------

func (s *HTTPServer) withAuth(next func(w http.ResponseWriter, r *http.Request, claims auth.Claims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, aerr := s.authenticate(r)
		if aerr != nil {
			writeJSON(w, envelope.Fail(nil, *aerr))
			return
		}
		next(w, r, claims)
	}
}

func (s *HTTPServer) authenticate(r *http.Request) (auth.Claims, *envelope.Error) {
	validator, verr := s.currentValidator()
	if verr != nil {
		return auth.Claims{}, verr
	}
	claims, err := validator.ParseBearer(r.Header.Get("Authorization"))
	if err != nil {
		e := toEnvelopeError(err)
		return auth.Claims{}, &e
	}
	return claims, nil
}

// currentValidator (re)builds a Validator from the currently loaded
// establishment config so key rotation and issuer/audience changes take
// effect without a server restart.
func (s *HTTPServer) currentValidator() (*auth.Validator, *envelope.Error) {
	if s.Engine.Cfg == nil {
		if err := s.Engine.Reload(); err != nil {
			return nil, &envelope.Error{Code: "configError", Message: err.Error()}
		}
	}
	validator, err := auth.NewValidator(s.Engine.Cfg.Auth)
	if err != nil {
		return nil, &envelope.Error{Code: "configError", Message: err.Error()}
	}
	return validator, nil
}

func toEnvelopeError(err error) envelope.Error {
	var aerr *auth.Error
	if errors.As(err, &aerr) {
		return envelope.Error{Code: aerr.Code, Message: aerr.Message}
	}
	return envelope.Error{Code: "invalidToken", Message: err.Error()}
}

// --- request helpers ---------------------------------------------------------

func isPlainTextUTF8(contentType string) bool {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || mt != "text/plain" {
		return false
	}
	if charset, ok := params["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	return true
}

func isJSONContentType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mt == "application/json"
}

// readBodyLimited reads r.Body up to limit bytes, returning a stable
// "bodyTooLarge" application error (HTTP 200, ok:false) rather than letting
// the transport fail the request outright.
func readBodyLimited(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, *envelope.Error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var mbErr *http.MaxBytesError
		if errors.As(err, &mbErr) {
			return nil, &envelope.Error{
				Code:    "bodyTooLarge",
				Message: fmt.Sprintf("request body exceeds the %d byte limit", limit),
			}
		}
		return nil, &envelope.Error{Code: "invalidRequest", Message: err.Error()}
	}
	return data, nil
}

func writeJSON(w http.ResponseWriter, result envelope.Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(result)
}
