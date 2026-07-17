package schemapatch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

const baseSchema = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "releaseYear", "kind": "number", "integer": true},
    {"name": "status", "kind": "string"},
    {"name": "highRated", "kind": "boolean", "default": false}
  ]
}`

const validULID = "01KWHM7R7D3T50G0GH6XN4CRZT"

func mustParseSpec(t *testing.T, raw string) *UpdateSpec {
	t.Helper()
	spec, err := ParseUpdateSpec([]byte(raw))
	if err != nil {
		t.Fatalf("ParseUpdateSpec: %v", err)
	}
	return spec
}

func TestParseUpdateSpecBasic(t *testing.T) {
	raw := `{
		"from": 0,
		"new_ver_id": "` + validULID + `",
		"updates": [
			{"op": "add", "path": "/rating", "value": 0, "schema": {"kind": "number", "default": 0}}
		]
	}`
	spec := mustParseSpec(t, raw)
	if spec.From != 0 {
		t.Fatalf("expected from=0, got %d", spec.From)
	}
	if spec.NewVerID != validULID {
		t.Fatalf("expected new_ver_id=%s, got %s", validULID, spec.NewVerID)
	}
	if len(spec.Updates) != 1 || spec.Updates[0].Op != "add" {
		t.Fatalf("unexpected updates: %#v", spec.Updates)
	}
}

func TestValidateStaleSchemaVersion(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 1, "new_ver_id": "`+validULID+`", "updates": [{"op":"remove","path":"/status"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "staleSchemaVersion") {
		t.Fatalf("expected staleSchemaVersion, got %#v", errs)
	}
}

func TestValidateBadToTarget(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "to": 5, "new_ver_id": "`+validULID+`", "updates": [{"op":"remove","path":"/status"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for bad to, got %#v", errs)
	}
}

func TestValidateBadULID(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "new_ver_id": "not-a-ulid", "updates": [{"op":"remove","path":"/status"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for bad ulid, got %#v", errs)
	}
}

func TestValidateEmptyUpdates(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "new_ver_id": "`+validULID+`", "updates": []}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for empty updates, got %#v", errs)
	}
}

func TestValidateUnsupportedOp(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "new_ver_id": "`+validULID+`", "updates": [{"op":"frobnicate","path":"/status"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for unsupported op, got %#v", errs)
	}
}

func TestValidateRejectsMetadataPath(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "new_ver_id": "`+validULID+`", "updates": [{"op":"remove","path":"/!"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for metadata path, got %#v", errs)
	}
}

func TestValidateRejectsArrayIndexPath(t *testing.T) {
	spec := mustParseSpec(t, `{"from": 0, "new_ver_id": "`+validULID+`", "updates": [{"op":"remove","path":"/reviews/4"}]}`)
	errs := spec.Validate(0)
	if !hasErrCode(errs, "invalidSchemaUpgrade") {
		t.Fatalf("expected invalidSchemaUpgrade for array index path, got %#v", errs)
	}
}

func TestApplyAdd(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "add", "path": "/rating", "value": 0, "schema": {"kind": "number", "default": 0}}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertHasField(t, out, "rating")
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
}

func TestApplyAddRejectsDuplicateField(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "add", "path": "/status", "schema": {"kind": "string"}}]
	}`)
	if _, err := Apply([]byte(baseSchema), spec); err == nil {
		t.Fatal("expected error for duplicate field")
	}
}

func TestApplyRemove(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "remove", "path": "/status"}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"status"`) {
		t.Fatalf("expected status removed: %s", out)
	}
}

func TestApplyAbandonRemovesField(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "abandon", "path": "/highRated"}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"highRated"`) {
		t.Fatalf("expected highRated removed: %s", out)
	}
}

func TestApplyMove(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "move", "from": "/status", "path": "/state"}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertHasField(t, out, "state")
	if strings.Contains(string(out), `"status"`) {
		t.Fatalf("expected status renamed away: %s", out)
	}
}

func TestApplyCopy(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "copy", "from": "/status", "path": "/statusCopy"}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertHasField(t, out, "statusCopy")
	assertHasField(t, out, "status")
}

func TestApplyConvert(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "convert", "path": "/releaseYear", "schema": {"kind": "string"}}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	children := doc["children"].([]any)
	found := false
	for _, c := range children {
		m := c.(map[string]any)
		if m["name"] == "releaseYear" {
			found = true
			if m["kind"] != "string" {
				t.Fatalf("expected releaseYear kind string, got %v", m["kind"])
			}
		}
	}
	if !found {
		t.Fatalf("releaseYear not found after convert: %s", out)
	}
}

func TestApplyReplaceIsNoStructuralChange(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "replace", "path": "/status", "value": "archived"}]
	}`)
	out, err := Apply([]byte(baseSchema), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertHasField(t, out, "status")
}

func TestApplyUnknownFieldFails(t *testing.T) {
	spec := mustParseSpec(t, `{
		"from": 0, "new_ver_id": "`+validULID+`",
		"updates": [{"op": "remove", "path": "/doesNotExist"}]
	}`)
	if _, err := Apply([]byte(baseSchema), spec); err == nil {
		t.Fatal("expected error removing unknown field")
	}
}

func assertHasField(t *testing.T, schemaBytes []byte, name string) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		t.Fatal(err)
	}
	children, _ := doc["children"].([]any)
	for _, c := range children {
		m, ok := c.(map[string]any)
		if ok && m["name"] == name {
			return
		}
	}
	t.Fatalf("expected field %q in schema: %s", name, schemaBytes)
}

func hasErrCode(errs []envelope.Error, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
