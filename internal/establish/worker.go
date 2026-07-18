// Package establish implements the non-establishment-server bootstrap and
// refresh flow described in tech-docs/AUTHENTICATION.md and
// tech-docs/ESTABLISHMENT-CONFIG.md: obtain and renew a machine token, fetch
// GET /datoriumdb/v1/establish, atomically cache the result under
// {config-dir}, and create any missing local collection directories.
//
// The establishment server itself never uses this package: per
// AUTHENTICATION.md's "Establishment Self-Start", it loads /db/.config
// locally and does not call HTTP /establish against itself.
package establish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/ojson"
)

const (
	defaultRefreshInterval = 60 * time.Second
	defaultRenewSkew       = 30 * time.Second
	defaultMaxFailures     = 5
	requestTimeout         = 15 * time.Second
)

// Worker fetches and caches establishment config for one non-establishment
// DatoriumDB server.
type Worker struct {
	ServerName       string
	EstablishmentURL string
	BootstrapSecret  string
	ConfigDir        string
	DataDir          string

	// HTTPClient defaults to a client with a bounded per-request timeout.
	HTTPClient *http.Client
	// RefreshInterval defaults to 60s (tech-docs/ESTABLISHMENT-CONFIG.md's
	// "reasonable MVP refresh policy").
	RefreshInterval time.Duration
	// RenewSkew controls how far ahead of expiry a machine token is
	// renewed. Defaults to 30s.
	RenewSkew time.Duration
	// MaxConsecutiveFailures triggers Run's OnFatal callback once
	// refreshes fail this many times in a row. Defaults to 5.
	MaxConsecutiveFailures int
	// Logger defaults to the standard logger.
	Logger *log.Logger

	mu             sync.Mutex
	token          string
	tokenExpiresAt time.Time

	wake chan struct{}
}

func (w *Worker) httpClient() *http.Client {
	if w.HTTPClient != nil {
		return w.HTTPClient
	}
	return &http.Client{Timeout: requestTimeout}
}

func (w *Worker) refreshInterval() time.Duration {
	if w.RefreshInterval > 0 {
		return w.RefreshInterval
	}
	return defaultRefreshInterval
}

func (w *Worker) renewSkew() time.Duration {
	if w.RenewSkew > 0 {
		return w.RenewSkew
	}
	return defaultRenewSkew
}

func (w *Worker) maxFailures() int {
	if w.MaxConsecutiveFailures > 0 {
		return w.MaxConsecutiveFailures
	}
	return defaultMaxFailures
}

func (w *Worker) logf(format string, args ...any) {
	if w.Logger != nil {
		w.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Wake schedules an immediate refresh, for event-driven cases described in
// ESTABLISHMENT-CONFIG.md ("Config Updates"): a machine refuses a command
// for the wrong shard, a config version is reported stale, or a connection
// to an expected target repeatedly fails. It never blocks.
func (w *Worker) Wake() {
	w.mu.Lock()
	ch := w.wake
	w.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// HasLocalConfig reports whether dir contains a structurally valid cached
// establishment config (all required files present and parseable).
func HasLocalConfig(dir string) bool {
	_, err := config.LoadUnvalidated(dir)
	return err == nil
}

// Bootstrap performs the startup flow from AUTHENTICATION.md's "Startup
// Flow": obtain a machine token, fetch /establish, and cache the result.
// If that fails but ConfigDir already holds an acceptable local cache, this
// still succeeds so the server can start from the cache while Run keeps
// retrying in the background, per AUTHENTICATION.md: "Cached config is
// acceptable only when all required files are present."
func (w *Worker) Bootstrap(ctx context.Context) error {
	err := w.RefreshOnce(ctx)
	if err == nil {
		return nil
	}
	if HasLocalConfig(w.ConfigDir) {
		w.logf("establishment bootstrap failed, continuing with cached config: %v", err)
		return nil
	}
	return fmt.Errorf("establishment bootstrap failed and no usable local config is cached: %w", err)
}

// Run refreshes establishment config on a timer and whenever Wake is
// called, until ctx is cancelled or refreshes fail
// MaxConsecutiveFailures times in a row, at which point onFatal is invoked
// (if non-nil) with the last error and Run returns.
func (w *Worker) Run(ctx context.Context, onFatal func(error)) {
	w.mu.Lock()
	if w.wake == nil {
		w.wake = make(chan struct{}, 1)
	}
	w.mu.Unlock()

	ticker := time.NewTicker(w.refreshInterval())
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
		if err := w.RefreshOnce(ctx); err != nil {
			consecutiveFailures++
			w.logf("establishment refresh failed (%d/%d consecutive): %v", consecutiveFailures, w.maxFailures(), err)
			if consecutiveFailures >= w.maxFailures() {
				if onFatal != nil {
					onFatal(err)
				}
				return
			}
			continue
		}
		consecutiveFailures = 0
	}
}

// Token ensures a fresh machine token for this server and returns it, so
// Worker can also act as a replication.TokenSource: the SOT-member and
// read-member server-to-server calls in tech-docs/SERVER-TO-SERVER-API.md
// reuse the same machine-token bootstrap/renewal flow used for
// establishment refresh.
func (w *Worker) Token(ctx context.Context) (string, error) {
	if err := w.ensureMachineToken(ctx); err != nil {
		return "", err
	}
	tok, _ := w.currentToken()
	return tok, nil
}

// RefreshOnce ensures a valid machine token, fetches /establish, atomically
// caches the response under ConfigDir, and creates any missing local
// collection directories under DataDir.
func (w *Worker) RefreshOnce(ctx context.Context) error {
	doc, err := w.fetchEstablishment(ctx)
	if err != nil {
		return err
	}
	if err := w.writeConfig(doc); err != nil {
		return fmt.Errorf("cache establishment config: %w", err)
	}
	if err := w.createCollectionDirs(doc); err != nil {
		return fmt.Errorf("create collection directories: %w", err)
	}
	return nil
}

// --- machine token acquisition / renewal -----------------------------------

type machineTokenRequest struct {
	ServerName      string `json:"serverName"`
	BootstrapSecret string `json:"bootstrapSecret,omitempty"`
}

type machineTokenResponse struct {
	OK        bool            `json:"ok"`
	Token     string          `json:"token"`
	ExpiresIn int             `json:"expiresIn"`
	Errors    []responseError `json:"errors"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (w *Worker) currentToken() (string, time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.token, w.tokenExpiresAt
}

func (w *Worker) setToken(token string, lifetime time.Duration) {
	w.mu.Lock()
	w.token = token
	w.tokenExpiresAt = time.Now().Add(lifetime)
	w.mu.Unlock()
}

// ensureMachineToken renews the cached machine token if it is missing or
// close to expiry. A still-valid token is renewed via the bearer-renewal
// path (no bootstrap secret needed); otherwise the bootstrap secret is
// used, per AUTHENTICATION.md's "Machine Token Endpoint".
func (w *Worker) ensureMachineToken(ctx context.Context) error {
	tok, exp := w.currentToken()
	now := time.Now()
	if tok != "" && now.Before(exp.Add(-w.renewSkew())) {
		return nil
	}

	req := machineTokenRequest{ServerName: w.ServerName}
	var authHeader string
	if tok != "" && now.Before(exp) {
		authHeader = "Bearer " + tok
	} else {
		req.BootstrapSecret = w.BootstrapSecret
	}

	token, expiresIn, err := w.callMachineToken(ctx, req, authHeader)
	if err != nil {
		return err
	}
	w.setToken(token, time.Duration(expiresIn)*time.Second)
	return nil
}

func (w *Worker) callMachineToken(ctx context.Context, body machineTokenRequest, authHeader string) (string, int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.EstablishmentURL+"/datoriumdb/v1/auth/machine-token", bytes.NewReader(buf))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := w.httpClient().Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("call machine-token endpoint: %w", err)
	}
	defer resp.Body.Close()

	var out machineTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("decode machine-token response (status %d): %w", resp.StatusCode, err)
	}
	if !out.OK {
		return "", 0, fmt.Errorf("machine-token request failed: %s", firstErrorMessage(out.Errors))
	}
	return out.Token, out.ExpiresIn, nil
}

func firstErrorMessage(errs []responseError) string {
	if len(errs) == 0 {
		return "unknown error"
	}
	return fmt.Sprintf("%s: %s", errs[0].Code, errs[0].Message)
}

// --- establishment fetch ----------------------------------------------------

type schemaEntry struct {
	Version int             `json:"version"`
	Schema  json.RawMessage `json:"schema"`
}

type establishResponse struct {
	OK       bool                                  `json:"ok"`
	General  json.RawMessage                       `json:"general"`
	Servers  json.RawMessage                       `json:"servers"`
	ShardMap json.RawMessage                       `json:"shardMap"`
	Auth     json.RawMessage                       `json:"auth"`
	Schemas  map[string]schemaEntry                `json:"schemas"`
	Searches map[string]map[string]json.RawMessage `json:"searches"`
	Errors   []responseError                       `json:"errors"`
}

func (w *Worker) fetchEstablishment(ctx context.Context) (*establishResponse, error) {
	if err := w.ensureMachineToken(ctx); err != nil {
		return nil, fmt.Errorf("obtain machine token: %w", err)
	}
	tok, _ := w.currentToken()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.EstablishmentURL+"/datoriumdb/v1/establish", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := w.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("call establish endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read establish response (status %d): %w", resp.StatusCode, err)
	}
	doc, err := parseEstablishResponse(body)
	if err != nil {
		return nil, fmt.Errorf("decode establish response (status %d): %w", resp.StatusCode, err)
	}
	if !doc.OK {
		return nil, fmt.Errorf("establish request failed: %s", firstErrorMessage(doc.Errors))
	}
	return doc, nil
}

func parseEstablishResponse(body []byte) (*establishResponse, error) {
	root, err := ojson.ReadBytesNoSchema(body)
	if err != nil {
		return nil, err
	}
	if !root.IsObject() {
		return nil, fmt.Errorf("establish response root must be an object")
	}
	doc := &establishResponse{
		Schemas:  map[string]schemaEntry{},
		Searches: map[string]map[string]json.RawMessage{},
	}
	ok, err := root.Get("ok").ToBoolTry()
	if err != nil {
		return nil, fmt.Errorf("missing or invalid ok field: %w", err)
	}
	doc.OK = ok
	if errs := root.Get("errors"); errs.IsArray() {
		for i := 0; i < errs.Len(); i++ {
			item := errs.At(i)
			doc.Errors = append(doc.Errors, responseError{
				Code:    item.Get("code").String(),
				Message: item.Get("message").String(),
			})
		}
	}
	if !doc.OK {
		return doc, nil
	}
	doc.General = rawJSONField(root, "general")
	doc.Servers = rawJSONField(root, "servers")
	doc.ShardMap = rawJSONField(root, "shardMap")
	doc.Auth = rawJSONField(root, "auth")

	for _, field := range root.Get("schemas").ObjectFields() {
		entryObj := field.Value
		ver, err := entryObj.Get("version").ToIntTry()
		if err != nil {
			return nil, fmt.Errorf("schemas.%s.version: %w", field.Key, err)
		}
		schemaVal := entryObj.Get("schema")
		if schemaVal.IsVoid() || schemaVal.IsMissing() {
			return nil, fmt.Errorf("schemas.%s.schema is required", field.Key)
		}
		doc.Schemas[field.Key] = schemaEntry{
			Version: ver,
			Schema:  json.RawMessage(schemaVal.ToJSONBytes()),
		}
	}
	for _, coll := range root.Get("searches").ObjectFields() {
		inner := map[string]json.RawMessage{}
		for _, search := range coll.Value.ObjectFields() {
			inner[search.Key] = json.RawMessage(search.Value.ToJSONBytes())
		}
		doc.Searches[coll.Key] = inner
	}
	return doc, nil
}

func rawJSONField(root ojson.JSONValue, key string) json.RawMessage {
	v := root.Get(key)
	if v.IsVoid() || v.IsMissing() {
		return json.RawMessage("{}")
	}
	return json.RawMessage(v.ToJSONBytes())
}

// --- local cache -------------------------------------------------------------

func (w *Worker) writeConfig(doc *establishResponse) error {
	if err := os.MkdirAll(w.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := writeWrapped(filepath.Join(w.ConfigDir, "__general.json"), "general", doc.General); err != nil {
		return err
	}
	if err := writeWrapped(filepath.Join(w.ConfigDir, "__servers.json"), "servers", doc.Servers); err != nil {
		return err
	}
	if err := writeWrapped(filepath.Join(w.ConfigDir, "__shard-map.json"), "shardMap", doc.ShardMap); err != nil {
		return err
	}
	if err := writeWrapped(filepath.Join(w.ConfigDir, "__auth.json"), "auth", doc.Auth); err != nil {
		return err
	}
	for name, entry := range doc.Schemas {
		if err := writeRaw(filepath.Join(w.ConfigDir, name+".schema.json"), entry.Schema); err != nil {
			return err
		}
		historic := filepath.Join(w.ConfigDir, fmt.Sprintf("%s.schema.%d.json", name, entry.Version))
		if err := writeRaw(historic, entry.Schema); err != nil {
			return err
		}
	}
	for collection, byName := range doc.Searches {
		for name, raw := range byName {
			path := filepath.Join(w.ConfigDir, fmt.Sprintf("%s.search.%s.json", collection, name))
			if err := writeRaw(path, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Worker) createCollectionDirs(doc *establishResponse) error {
	for name := range doc.Schemas {
		if err := fsstore.EnsureCollectionDir(w.DataDir, name); err != nil {
			return err
		}
	}
	return nil
}

func writeWrapped(path, key string, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	inner, err := ojson.ReadBytesNoSchema(raw)
	if err != nil {
		return err
	}
	root := ojson.NewObject()
	root.Set(key, inner)
	pretty, err := config.PrettyJSONBytes(root.ToJSONBytes())
	if err != nil {
		return err
	}
	return fsstore.WriteFileAtomic(path, pretty, 0o644)
}

func writeRaw(path string, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	pretty, err := config.PrettyJSONBytes(raw)
	if err != nil {
		return err
	}
	return fsstore.WriteFileAtomic(path, pretty, 0o644)
}
