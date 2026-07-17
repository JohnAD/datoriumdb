//go:build contract

package contract

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// newContractEngine returns a single-node engine.Engine loaded from a
// fresh copy of testdata/sample-config, whose shard map assigns the whole
// keyspace's SOT and read roles to "serverA".
func newContractEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng := &engine.Engine{
		ConfigDir:  testutil.TempConfigDir(t),
		DataDir:    testutil.TempDataDir(t),
		ServerName: "serverA",
	}
	if err := eng.Reload(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return eng
}

func TestGoldenCreateOK(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`create Movies null {$: Movies:0, title: "The Matrix"}`)
	if res["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", res)
	}
	AssertGolden(t, "create_ok", res)
}

func TestGoldenCreateDocumentExists(t *testing.T) {
	eng := newContractEngine(t)
	first := eng.Execute(`create Movies fixedid001 {$: Movies:0, title: "The Matrix"}`)
	if first["ok"] != true {
		t.Fatalf("expected first create to succeed: %#v", first)
	}
	res := eng.Execute(`create Movies fixedid001 {$: Movies:0, title: "Duplicate"}`)
	if res["ok"] != false {
		t.Fatalf("expected duplicate create to fail: %#v", res)
	}
	AssertGolden(t, "create_document_exists", res)
}

func TestGoldenCreateInvalidDocumentID(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`create Movies "not/safe" {$: Movies:0, title: "The Matrix"}`)
	if res["ok"] != false {
		t.Fatalf("expected invalid id create to fail: %#v", res)
	}
	AssertGolden(t, "create_invalid_document_id", res)
}

func TestGoldenCreateSchemaMismatch(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`create Movies null {$: Movies:99, title: "The Matrix"}`)
	if res["ok"] != false {
		t.Fatalf("expected schema mismatch to fail: %#v", res)
	}
	AssertGolden(t, "create_schema_mismatch", res)
}

func TestGoldenReadNotFound(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`read Movies doesNotExist001 {}`)
	if res["ok"] != false {
		t.Fatalf("expected read of missing document to fail: %#v", res)
	}
	AssertGolden(t, "read_not_found", res)
}

func TestGoldenReadOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(`create Movies fixedid002 {$: Movies:0, title: "Arrival", releaseYear: 2016}`)
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(`read Movies fixedid002 {}`)
	if res["ok"] != true {
		t.Fatalf("expected read to succeed: %#v", res)
	}
	AssertGolden(t, "read_ok", res)
}

func TestGoldenPatchOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(`create Movies fixedid003 {$: Movies:0, title: "Interstellar"}`)
	ver, _ := created["#"].(string)
	res := eng.Execute(`patch Movies fixedid003 {$: Movies:0, #: ` + ver + `, RFC6902: [{op: add, path: /status, value: released}]}`)
	if res["ok"] != true {
		t.Fatalf("expected patch to succeed: %#v", res)
	}
	AssertGolden(t, "patch_ok", res)
}

func TestGoldenPatchVersionMismatch(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(`create Movies fixedid004 {$: Movies:0, title: "Interstellar"}`)
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(`patch Movies fixedid004 {$: Movies:0, #: notTheRealVersion, RFC6902: [{op: add, path: /status, value: released}]}`)
	if res["ok"] != false {
		t.Fatalf("expected version mismatch to fail: %#v", res)
	}
	AssertGolden(t, "patch_version_mismatch", res)
}

func TestGoldenDeleteOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(`create Movies fixedid005 {$: Movies:0, title: "Arrival"}`)
	ver, _ := created["#"].(string)
	res := eng.Execute(`delete Movies fixedid005 {#: ` + ver + `}`)
	if res["ok"] != true {
		t.Fatalf("expected delete to succeed: %#v", res)
	}
	AssertGolden(t, "delete_ok", res)
}

func TestGoldenDeleteVersionMismatch(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(`create Movies fixedid006 {$: Movies:0, title: "Arrival"}`)
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(`delete Movies fixedid006 {#: notTheRealVersion}`)
	if res["ok"] != false {
		t.Fatalf("expected delete version mismatch to fail: %#v", res)
	}
	AssertGolden(t, "delete_version_mismatch", res)
}

func TestGoldenCollectionNotFound(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`create NoSuchCollection null {$: NoSuchCollection:0, title: "x"}`)
	if res["ok"] != false {
		t.Fatalf("expected unknown collection to fail: %#v", res)
	}
	AssertGolden(t, "create_collection_not_found", res)
}

// TestGoldenWrongMachine exercises SHARDING.md's wrongMachine response
// shape: a server that is not the shard's SHARD_SOT_MEMBER refuses
// create/patch/delete with routing hints (shardSlot, correctServer,
// baseURL, configVersion).
func TestGoldenWrongMachine(t *testing.T) {
	eng := newContractEngine(t)
	eng.ServerName = "notTheSOTMember"
	// A fixed document ID keeps the resulting shard slot (a CRC32 hash of
	// the ID) stable across runs, unlike the auto-generated ULID "null"
	// would produce.
	res := eng.Execute(`create Movies fixedid007 {$: Movies:0, title: "The Matrix"}`)
	if res["ok"] != false {
		t.Fatalf("expected wrong-machine create to fail: %#v", res)
	}
	if res["errors"] == nil {
		t.Fatalf("expected errors array: %#v", res)
	}
	AssertGolden(t, "create_wrong_machine", res)
}

func TestGoldenUnknownCommand(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(`frobnicate Movies null {}`)
	if res["ok"] != false {
		t.Fatalf("expected unknown command to fail: %#v", res)
	}
	AssertGolden(t, "unknown_command", res)
}
