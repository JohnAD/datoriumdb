package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/shard"
)

// General is the __general.json shape.
type General struct {
	General GeneralBody `json:"general"`
}

// GeneralBody is the "general" object inside __general.json.
type GeneralBody struct {
	Name                                string `json:"name"`
	EstablishmentServer                 string `json:"establishmentServer"`
	Version                             int    `json:"version"`
	ReadMemberCheckinSeconds            int    `json:"readMemberCheckinSeconds"`
	CacheUpdateCheckinSeconds           int    `json:"cacheUpdateCheckinSeconds"`
	ReadMemberFailedCheckinsBeforeStale int    `json:"readMemberFailedCheckinsBeforeStale"`
}

// ServerEntry is one server definition.
type ServerEntry struct {
	BaseURL string `json:"baseURL"`
}

// ServersFile is __servers.json.
type ServersFile struct {
	Servers map[string]ServerEntry `json:"servers"`
}

// ShardAssignment is one shard range assignment.
type ShardAssignment struct {
	ShardSOTMember  string   `json:"SHARD_SOT_MEMBER"`
	ShardReadMember []string `json:"SHARD_READ_MEMBER"`
	ProxyReadMember []string `json:"PROXY_READ_MEMBER"`
}

// ShardMapBody is the "shardMap" object.
type ShardMapBody struct {
	Default map[string]ShardAssignment `json:"default"`
}

// ShardMapFile is __shard-map.json.
type ShardMapFile struct {
	ShardMap ShardMapBody `json:"shardMap"`
}

// SearchEntry is a compact summary of a stored search definition file, kept
// generic since the CLI mostly treats search bodies opaquely.
type SearchEntry struct {
	Schema     string          `json:"$"`
	Collection string          `json:"collection"`
	Name       string          `json:"name"`
	Version    int             `json:"version"`
	Raw        json.RawMessage `json:"-"`
}

// Config is the loaded establishment config directory.
type Config struct {
	Dir      string
	General  General
	Servers  ServersFile
	ShardMap ShardMapFile
	Auth     AuthFile

	Schemas        map[string]json.RawMessage
	SchemaVersions map[string]int
	SchemaHistory  map[string]map[int]json.RawMessage
	// SchemaUpdateHistory holds the persisted per-version update-list file
	// (the exact add/import/remove/abandon/replace/move/copy/convert ops
	// from UPDATE-SCHEMA.md) for each schema version produced by
	// `datoriumctl collection upgrade`, keyed by the version it produced.
	// Version 0 (the collection's initial schema) never has an entry.
	// This lets the change/upgrade agents replay per-document migration
	// steps instead of only having the before/after schema JSON.
	SchemaUpdateHistory map[string]map[int]json.RawMessage

	Searches       map[string]map[string]json.RawMessage
	SearchVersions map[string]map[string]int
	SearchHistory  map[string]map[string]map[int]json.RawMessage
}

// Load reads and strictly validates a config directory. It returns an error
// combining every validation failure found.
func Load(dir string) (*Config, error) {
	cfg, err := loadUnvalidated(dir)
	if err != nil {
		return nil, err
	}
	if errs := cfg.ValidateDetailed(); len(errs) > 0 {
		return nil, combineErrors(errs)
	}
	return cfg, nil
}

// LoadUnvalidated reads a config directory without running full validation.
// This is used by mutating CLI commands that need to apply a change in
// memory before validating the complete candidate config.
func LoadUnvalidated(dir string) (*Config, error) {
	return loadUnvalidated(dir)
}

func loadUnvalidated(dir string) (*Config, error) {
	cfg := &Config{
		Dir:                 dir,
		Schemas:             map[string]json.RawMessage{},
		SchemaVersions:      map[string]int{},
		SchemaHistory:       map[string]map[int]json.RawMessage{},
		SchemaUpdateHistory: map[string]map[int]json.RawMessage{},
		Searches:            map[string]map[string]json.RawMessage{},
		SearchVersions:      map[string]map[string]int{},
		SearchHistory:       map[string]map[string]map[int]json.RawMessage{},
	}
	if err := readJSON(filepath.Join(dir, "__general.json"), &cfg.General); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "__servers.json"), &cfg.Servers); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "__shard-map.json"), &cfg.ShardMap); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "__auth.json"), &cfg.Auth); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasPrefix(name, "__") {
			continue
		}
		if collection, search, ver, ok := parseVersionedSearchFilename(name); ok {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if cfg.SearchHistory[collection] == nil {
				cfg.SearchHistory[collection] = map[string]map[int]json.RawMessage{}
			}
			if cfg.SearchHistory[collection][search] == nil {
				cfg.SearchHistory[collection][search] = map[int]json.RawMessage{}
			}
			cfg.SearchHistory[collection][search][ver] = json.RawMessage(raw)
			continue
		}
		if collection, search, ok := parseSearchFilename(name); ok {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if cfg.Searches[collection] == nil {
				cfg.Searches[collection] = map[string]json.RawMessage{}
			}
			cfg.Searches[collection][search] = json.RawMessage(raw)
			continue
		}
		if ver, collection, ok := parseVersionedSchemaUpdateFilename(name); ok {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if cfg.SchemaUpdateHistory[collection] == nil {
				cfg.SchemaUpdateHistory[collection] = map[int]json.RawMessage{}
			}
			cfg.SchemaUpdateHistory[collection][ver] = json.RawMessage(raw)
			continue
		}
		if ver, collection, ok := parseVersionedSchemaFilename(name); ok {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if cfg.SchemaHistory[collection] == nil {
				cfg.SchemaHistory[collection] = map[int]json.RawMessage{}
			}
			cfg.SchemaHistory[collection][ver] = json.RawMessage(raw)
			continue
		}
		if strings.HasSuffix(name, ".schema.json") {
			collection := strings.TrimSuffix(name, ".schema.json")
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			cfg.Schemas[collection] = json.RawMessage(raw)
			continue
		}
	}
	cfg.deriveSchemaVersions()
	cfg.deriveSearchVersions()
	return cfg, nil
}

func parseVersionedSchemaFilename(name string) (ver int, collection string, ok bool) {
	// Movies.schema.1.json
	if !strings.HasSuffix(name, ".json") {
		return 0, "", false
	}
	base := strings.TrimSuffix(name, ".json")
	parts := strings.Split(base, ".")
	if len(parts) != 3 || parts[1] != "schema" || !looksInt(parts[2]) {
		return 0, "", false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, "", false
	}
	return n, parts[0], true
}

func parseVersionedSchemaUpdateFilename(name string) (ver int, collection string, ok bool) {
	// Movies.schema.1.update.json
	if !strings.HasSuffix(name, ".update.json") {
		return 0, "", false
	}
	base := strings.TrimSuffix(name, ".update.json")
	parts := strings.Split(base, ".")
	if len(parts) != 3 || parts[1] != "schema" || !looksInt(parts[2]) {
		return 0, "", false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, "", false
	}
	return n, parts[0], true
}

func parseSearchFilename(name string) (collection, search string, ok bool) {
	// Movies.search.byReleasedGenre.json
	if !strings.HasSuffix(name, ".json") {
		return "", "", false
	}
	base := strings.TrimSuffix(name, ".json")
	parts := strings.Split(base, ".")
	if len(parts) != 3 || parts[1] != "search" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func parseVersionedSearchFilename(name string) (collection, search string, ver int, ok bool) {
	// Movies.search.byReleasedGenre.1.json
	if !strings.HasSuffix(name, ".json") {
		return "", "", 0, false
	}
	base := strings.TrimSuffix(name, ".json")
	parts := strings.Split(base, ".")
	if len(parts) != 4 || parts[1] != "search" || !looksInt(parts[3]) {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, false
	}
	return parts[0], parts[2], n, true
}

func (c *Config) deriveSchemaVersions() {
	for collection := range c.Schemas {
		maxVer := 0
		found := false
		for ver := range c.SchemaHistory[collection] {
			if !found || ver > maxVer {
				maxVer = ver
				found = true
			}
		}
		c.SchemaVersions[collection] = maxVer
	}
}

func (c *Config) deriveSearchVersions() {
	for collection, byName := range c.Searches {
		if c.SearchVersions[collection] == nil {
			c.SearchVersions[collection] = map[string]int{}
		}
		for name := range byName {
			maxVer := 0
			found := false
			for ver := range c.SearchHistory[collection][name] {
				if !found || ver > maxVer {
					maxVer = ver
					found = true
				}
			}
			c.SearchVersions[collection][name] = maxVer
		}
	}
}

// SchemaVersion returns the current schema version for a collection.
func (c *Config) SchemaVersion(collection string) int {
	if c == nil {
		return 0
	}
	if v, ok := c.SchemaVersions[collection]; ok {
		return v
	}
	return 0
}

func looksInt(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Validate checks required establishment invariants, returning the first
// failure as a plain error. Prefer ValidateDetailed for full reporting.
func (c *Config) Validate() error {
	if errs := c.ValidateDetailed(); len(errs) > 0 {
		return combineErrors(errs)
	}
	return nil
}

// ValidateDetailed runs every COMMAND-LINE-TOOLS.md "complete config
// validation" rule and returns every failure found, using the stable error
// codes documented there.
func (c *Config) ValidateDetailed() []envelope.Error {
	var errs []envelope.Error

	if c.General.General.Name == "" {
		errs = append(errs, envelope.Error{Code: "invalidGeneralConfig", Path: "/general/name", Message: "general.name is required"})
	}
	if c.General.General.EstablishmentServer == "" {
		errs = append(errs, envelope.Error{Code: "invalidGeneralConfig", Path: "/general/establishmentServer", Message: "general.establishmentServer is required"})
	} else if _, ok := c.Servers.Servers[c.General.General.EstablishmentServer]; !ok {
		errs = append(errs, envelope.Error{
			Code:    "serverNotFound",
			Path:    "/general/establishmentServer",
			Message: fmt.Sprintf("establishmentServer %q is not a known server", c.General.General.EstablishmentServer),
			Actual:  c.General.General.EstablishmentServer,
		})
	}

	if len(c.Servers.Servers) == 0 {
		errs = append(errs, envelope.Error{Code: "invalidServersConfig", Path: "/servers", Message: "at least one server is required"})
	}
	for name, srv := range c.Servers.Servers {
		if !ValidBaseURL(srv.BaseURL) {
			errs = append(errs, envelope.Error{
				Code:    "invalidBaseURL",
				Path:    fmt.Sprintf("/servers/%s/baseURL", name),
				Message: "baseURL must be an absolute URL with scheme and host",
				Actual:  srv.BaseURL,
			})
		}
	}

	errs = append(errs, c.validateShardMap()...)
	errs = append(errs, c.validateAuth()...)
	errs = append(errs, c.validateSchemas()...)
	errs = append(errs, c.validateSearches()...)

	return errs
}

func (c *Config) validateShardMap() []envelope.Error {
	return ValidateShardMapBody(c.ShardMap.ShardMap, c.Servers.Servers)
}

// ValidateShardMapBody validates a shardMap.default body against a set of
// known servers: full slot coverage, no overlapping ranges, and every
// referenced server name must be known.
func ValidateShardMapBody(body ShardMapBody, servers map[string]ServerEntry) []envelope.Error {
	var errs []envelope.Error
	if body.Default == nil {
		return []envelope.Error{{Code: "incompleteShardMap", Path: "/shardMap/default", Message: "shardMap.default is required"}}
	}
	var ranges []shard.Range
	for raw, assignment := range body.Default {
		r, err := shard.ParseRange(raw)
		if err != nil {
			errs = append(errs, envelope.Error{Code: "invalidShardRange", Path: "/shardMap/default/" + raw, Message: err.Error()})
			continue
		}
		ranges = append(ranges, r)
		if assignment.ShardSOTMember == "" {
			errs = append(errs, envelope.Error{Code: "unknownServerReference", Path: "/shardMap/default/" + raw + "/SHARD_SOT_MEMBER", Message: "SHARD_SOT_MEMBER is required"})
		} else if _, ok := servers[assignment.ShardSOTMember]; !ok {
			errs = append(errs, envelope.Error{Code: "unknownServerReference", Path: "/shardMap/default/" + raw + "/SHARD_SOT_MEMBER", Message: "unknown server", Actual: assignment.ShardSOTMember})
		}
		for _, name := range assignment.ShardReadMember {
			if _, ok := servers[name]; !ok {
				errs = append(errs, envelope.Error{Code: "unknownServerReference", Path: "/shardMap/default/" + raw + "/SHARD_READ_MEMBER", Message: "unknown server", Actual: name})
			}
		}
		for _, name := range assignment.ProxyReadMember {
			if _, ok := servers[name]; !ok {
				errs = append(errs, envelope.Error{Code: "unknownServerReference", Path: "/shardMap/default/" + raw + "/PROXY_READ_MEMBER", Message: "unknown server", Actual: name})
			}
		}
	}
	if err := shard.ValidateFullCoverage(ranges); err != nil {
		code := "incompleteShardMap"
		if strings.Contains(err.Error(), "overlap") {
			code = "overlappingShardRanges"
		}
		errs = append(errs, envelope.Error{Code: code, Path: "/shardMap/default", Message: err.Error()})
	}
	return errs
}

func (c *Config) validateAuth() []envelope.Error {
	var errs []envelope.Error
	if c.Auth.Auth.Issuer == "" {
		errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: "/auth/issuer", Message: "auth.issuer is required"})
	}
	if c.Auth.Auth.Audience == "" {
		errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: "/auth/audience", Message: "auth.audience is required"})
	}
	if len(c.Auth.Auth.Keys) == 0 {
		errs = append(errs, envelope.Error{Code: "noActiveSigningKey", Path: "/auth/keys", Message: "at least one public signing key is required"})
	}
	seen := map[string]bool{}
	for i, k := range c.Auth.Auth.Keys {
		path := fmt.Sprintf("/auth/keys/%d", i)
		if k.Kid == "" {
			errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: path + "/kid", Message: "kid is required"})
		} else if seen[k.Kid] {
			errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: path + "/kid", Message: "duplicate kid", Actual: k.Kid})
		}
		seen[k.Kid] = true
		if k.Alg == "" {
			errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: path + "/alg", Message: "alg is required"})
		}
		if k.Status != "active" && k.Status != "retired" {
			errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: path + "/status", Message: "status must be active or retired", Actual: k.Status})
		}
		if k.PublicKey == "" {
			errs = append(errs, envelope.Error{Code: "invalidAuthConfig", Path: path + "/publicKey", Message: "publicKey is required"})
		} else if LooksLikePrivateKeyMaterial(k.PublicKey) {
			errs = append(errs, envelope.Error{Code: "privateKeyRejected", Path: path + "/publicKey", Message: "publicKey field looks like private key material"})
		}
	}
	if c.Auth.ActiveKeyCount() == 0 && len(c.Auth.Auth.Keys) > 0 {
		errs = append(errs, envelope.Error{Code: "noActiveSigningKey", Path: "/auth/keys", Message: "at least one key must have status active"})
	}
	return errs
}

func (c *Config) validateSchemas() []envelope.Error {
	var errs []envelope.Error
	for collection, raw := range c.Schemas {
		path := "/" + collection + ".schema.json"
		if !ValidCollectionName(collection) {
			errs = append(errs, envelope.Error{Code: "invalidCollectionName", Path: path, Message: "collection name violates naming conventions", Actual: collection})
		}
		if err := ValidateOJSONSchemaBytes(raw); err != nil {
			errs = append(errs, envelope.Error{Code: "invalidSchema", Path: path, Message: err.Error()})
			continue
		}
		ver, hasHistory := latestHistoryVersion(c.SchemaHistory[collection])
		if !hasHistory {
			errs = append(errs, envelope.Error{Code: "invalidSchema", Path: path, Message: "collection current schema has no matching versioned history file"})
			continue
		}
		historyRaw := c.SchemaHistory[collection][ver]
		if !jsonEqual(raw, historyRaw) {
			errs = append(errs, envelope.Error{
				Code:    "invalidSchema",
				Path:    fmt.Sprintf("/%s.schema.%d.json", collection, ver),
				Message: "current schema does not match its versioned history file",
			})
		}
	}
	return errs
}

func (c *Config) validateSearches() []envelope.Error {
	var errs []envelope.Error
	for collection, byName := range c.Searches {
		for name, raw := range byName {
			path := fmt.Sprintf("/%s.search.%s.json", collection, name)
			if !ValidSearchName(name) {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path, Message: "search name violates naming conventions", Actual: name})
			}
			var doc SearchEntry
			if err := json.Unmarshal(raw, &doc); err != nil {
				errs = append(errs, envelope.Error{Code: "invalidJSON", Path: path, Message: err.Error()})
				continue
			}
			if _, ok := c.Schemas[collection]; !ok {
				errs = append(errs, envelope.Error{Code: "collectionNotFound", Path: path + "/collection", Message: "search collection does not exist", Actual: collection})
			}
			if doc.Version <= 0 {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/version", Message: "version must be a positive integer", Actual: doc.Version})
			}
		}
	}
	return errs
}

func latestHistoryVersion(history map[int]json.RawMessage) (int, bool) {
	maxVer := 0
	found := false
	for ver := range history {
		if !found || ver > maxVer {
			maxVer = ver
			found = true
		}
	}
	return maxVer, found
}

func jsonEqual(a, b json.RawMessage) bool {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	ba, _ := json.Marshal(va)
	bb, _ := json.Marshal(vb)
	return string(ba) == string(bb)
}

func combineErrors(errs []envelope.Error) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Path != "" {
			parts = append(parts, fmt.Sprintf("%s (%s): %s", e.Code, e.Path, e.Message))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", e.Code, e.Message))
		}
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// EstablishDocument builds the combined establishment response body fields.
// Schema and search definition bodies are embedded as json.RawMessage so
// encoding/json does not reparse them into map[string]any (which would
// destroy field order). Callers that write those bodies to disk should
// pretty-print with PrettyJSONBytes (OJSON).
func (c *Config) EstablishDocument() map[string]any {
	schemas := map[string]any{}
	for name, raw := range c.Schemas {
		schemas[name] = map[string]any{
			"version": c.SchemaVersion(name),
			"schema":  json.RawMessage(raw),
		}
	}
	searches := map[string]any{}
	for collection, byName := range c.Searches {
		inner := map[string]any{}
		for name, raw := range byName {
			inner[name] = json.RawMessage(raw)
		}
		searches[collection] = inner
	}
	return map[string]any{
		"general":  c.General.General,
		"servers":  c.Servers.Servers,
		"shardMap": c.ShardMap.ShardMap,
		"schemas":  schemas,
		"searches": searches,
		"auth":     c.Auth.Auth,
	}
}

func readJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
