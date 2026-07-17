package config

import (
	"encoding/json"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func TestValidCollectionName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"Movies", true},
		{"People", true},
		{"height_units", true},
		{"", false},
		{"1Movies", false},
		{" Movies", false},
		{"Movi es", false},
		{"Movi__es", false},
		{"Movi_es", true},
	}
	for _, c := range cases {
		if got := ValidCollectionName(c.name); got != c.ok {
			t.Errorf("ValidCollectionName(%q) = %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestValidBaseURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://db.example.com", true},
		{"http://127.0.0.1:8080", true},
		{"", false},
		{"not-a-url", false},
		{"/relative/path", false},
		{"ftp://example.com", true}, // scheme+host present; CLI only checks absolute URL shape
	}
	for _, c := range cases {
		if got := ValidBaseURL(c.url); got != c.ok {
			t.Errorf("ValidBaseURL(%q) = %v, want %v", c.url, got, c.ok)
		}
	}
}

func baseValidConfig() *Config {
	cfg := &Config{
		Dir: "/tmp/does-not-matter",
		General: General{General: GeneralBody{
			Name:                                "Test DB",
			EstablishmentServer:                 "serverA",
			Version:                             1,
			ReadMemberCheckinSeconds:            10,
			CacheUpdateCheckinSeconds:           60,
			ReadMemberFailedCheckinsBeforeStale: 3,
		}},
		Servers: ServersFile{Servers: map[string]ServerEntry{
			"serverA": {BaseURL: "https://serverA.example.com"},
		}},
		ShardMap: ShardMapFile{ShardMap: ShardMapBody{Default: map[string]ShardAssignment{
			"00-FF": {ShardSOTMember: "serverA", ShardReadMember: []string{"serverA"}, ProxyReadMember: []string{}},
		}}},
		Auth: AuthFile{Auth: AuthBody{
			Issuer:   "https://issuer.test",
			Audience: "datoriumdb",
			Keys: []AuthKey{
				{Kid: "k1", Alg: "EdDSA", Status: "active", PublicKey: "dGVzdC1wdWJsaWMta2V5"},
			},
		}},
		Schemas:        map[string]json.RawMessage{},
		SchemaVersions: map[string]int{},
		SchemaHistory:  map[string]map[int]json.RawMessage{},
		Searches:       map[string]map[string]json.RawMessage{},
		SearchVersions: map[string]map[string]int{},
		SearchHistory:  map[string]map[string]map[int]json.RawMessage{},
	}
	return cfg
}

func TestValidateDetailedBaseIsValid(t *testing.T) {
	cfg := baseValidConfig()
	if errs := cfg.ValidateDetailed(); len(errs) != 0 {
		t.Fatalf("expected valid config, got errors: %#v", errs)
	}
}

func TestValidateDetailedUnknownEstablishmentServer(t *testing.T) {
	cfg := baseValidConfig()
	cfg.General.General.EstablishmentServer = "serverZ"
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "serverNotFound") {
		t.Fatalf("expected serverNotFound, got %#v", errs)
	}
}

func TestValidateDetailedInvalidBaseURL(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Servers.Servers["serverA"] = ServerEntry{BaseURL: "not-a-url"}
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "invalidBaseURL") {
		t.Fatalf("expected invalidBaseURL, got %#v", errs)
	}
}

func TestValidateDetailedIncompleteShardMap(t *testing.T) {
	cfg := baseValidConfig()
	cfg.ShardMap.ShardMap.Default = map[string]ShardAssignment{
		"00-7E": {ShardSOTMember: "serverA"},
	}
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "incompleteShardMap") {
		t.Fatalf("expected incompleteShardMap, got %#v", errs)
	}
}

func TestValidateDetailedOverlappingShardRanges(t *testing.T) {
	cfg := baseValidConfig()
	cfg.ShardMap.ShardMap.Default = map[string]ShardAssignment{
		"00-80": {ShardSOTMember: "serverA"},
		"70-FF": {ShardSOTMember: "serverA"},
	}
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "overlappingShardRanges") {
		t.Fatalf("expected overlappingShardRanges, got %#v", errs)
	}
}

func TestValidateDetailedUnknownServerReference(t *testing.T) {
	cfg := baseValidConfig()
	cfg.ShardMap.ShardMap.Default = map[string]ShardAssignment{
		"00-FF": {ShardSOTMember: "serverGhost"},
	}
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "unknownServerReference") {
		t.Fatalf("expected unknownServerReference, got %#v", errs)
	}
}

func TestValidateDetailedNoActiveSigningKey(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.Auth.Keys[0].Status = "retired"
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "noActiveSigningKey") {
		t.Fatalf("expected noActiveSigningKey, got %#v", errs)
	}
}

func TestValidateDetailedPrivateKeyRejected(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Auth.Auth.Keys[0].PublicKey = "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "privateKeyRejected") {
		t.Fatalf("expected privateKeyRejected, got %#v", errs)
	}
}

func TestValidateDetailedSchemaHistoryMismatch(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Schemas["Movies"] = json.RawMessage(`{"kind":"object","children":[]}`)
	cfg.SchemaHistory["Movies"] = map[int]json.RawMessage{
		0: json.RawMessage(`{"kind":"object","children":[{"name":"title","kind":"string"}]}`),
	}
	cfg.SchemaVersions["Movies"] = 0
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for schema history mismatch, got %#v", errs)
	}
}

func TestValidateDetailedSchemaMissingHistory(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Schemas["Movies"] = json.RawMessage(`{"kind":"object","children":[]}`)
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for missing history, got %#v", errs)
	}
}

func TestValidateDetailedInvalidCollectionName(t *testing.T) {
	cfg := baseValidConfig()
	body := json.RawMessage(`{"kind":"object","children":[]}`)
	cfg.Schemas["not valid"] = body
	cfg.SchemaHistory["not valid"] = map[int]json.RawMessage{0: body}
	cfg.SchemaVersions["not valid"] = 0
	errs := cfg.ValidateDetailed()
	if !hasCode(errs, "invalidCollectionName") {
		t.Fatalf("expected invalidCollectionName, got %#v", errs)
	}
}

func hasCode(errs []envelope.Error, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
