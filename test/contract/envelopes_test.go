//go:build contract

package contract

import (
	"strings"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/commandreq"
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
	res := eng.Execute(commandreq.Must("create", "Movies", "01TESTMOVIES00000000000001", map[string]any{"$": "Movies:0", "title": "The Matrix"}))
	if res["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", res)
	}
	AssertGolden(t, "create_ok", res)
}

func TestGoldenCreateDocumentExists(t *testing.T) {
	eng := newContractEngine(t)
	first := eng.Execute(commandreq.Must("create", "Movies", "fixedid001", map[string]any{"$": "Movies:0", "title": "The Matrix"}))
	if first["ok"] != true {
		t.Fatalf("expected first create to succeed: %#v", first)
	}
	res := eng.Execute(commandreq.Must("create", "Movies", "fixedid001", map[string]any{"$": "Movies:0", "title": "Duplicate"}))
	if res["ok"] != false {
		t.Fatalf("expected duplicate create to fail: %#v", res)
	}
	AssertGolden(t, "create_document_exists", res)
}

func TestGoldenCreateInvalidDocumentID(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(commandreq.Must("create", "Movies", "not/safe", map[string]any{"$": "Movies:0", "title": "The Matrix"}))
	if res["ok"] != false {
		t.Fatalf("expected invalid id create to fail: %#v", res)
	}
	AssertGolden(t, "create_invalid_document_id", res)
}

func TestGoldenCreateSchemaMismatch(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(commandreq.Must("create", "Movies", "01TESTMOVIES00000000000002", map[string]any{"$": "Movies:99", "title": "The Matrix"}))
	if res["ok"] != false {
		t.Fatalf("expected schema mismatch to fail: %#v", res)
	}
	AssertGolden(t, "create_schema_mismatch", res)
}

func TestGoldenReadNotFound(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(commandreq.Must("read", "Movies", "doesNotExist001", map[string]any{}))
	if res["ok"] != false {
		t.Fatalf("expected read of missing document to fail: %#v", res)
	}
	AssertGolden(t, "read_not_found", res)
}

func TestGoldenReadOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedid002", map[string]any{"$": "Movies:0", "title": "Arrival", "releaseYear": 2016}))
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(commandreq.Must("read", "Movies", "fixedid002", map[string]any{}))
	if res["ok"] != true {
		t.Fatalf("expected read to succeed: %#v", res)
	}
	AssertGolden(t, "read_ok", res)
}

func TestGoldenPatchOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedid003", map[string]any{"$": "Movies:0", "title": "Interstellar"}))
	ver, _ := created["#"].(string)
	res := eng.Execute(commandreq.Must("patch", "Movies", "fixedid003", map[string]any{"$": "Movies:0", "#": ver, "RFC6902": []any{map[string]any{"op": "add", "path": "/status", "value": "released"}}}))
	if res["ok"] != true {
		t.Fatalf("expected patch to succeed: %#v", res)
	}
	AssertGolden(t, "patch_ok", res)
}

func TestGoldenPatchVersionMismatch(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedid004", map[string]any{"$": "Movies:0", "title": "Interstellar"}))
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(commandreq.Must("patch", "Movies", "fixedid004", map[string]any{"$": "Movies:0", "#": "notTheRealVersion", "RFC6902": []any{map[string]any{"op": "add", "path": "/status", "value": "released"}}}))
	if res["ok"] != false {
		t.Fatalf("expected version mismatch to fail: %#v", res)
	}
	AssertGolden(t, "patch_version_mismatch", res)
}

func TestGoldenDeleteOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedid005", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	ver, _ := created["#"].(string)
	res := eng.Execute(commandreq.Must("delete", "Movies", "fixedid005", map[string]any{"#": ver}))
	if res["ok"] != true {
		t.Fatalf("expected delete to succeed: %#v", res)
	}
	AssertGolden(t, "delete_ok", res)
}

func TestGoldenDeleteVersionMismatch(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedid006", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.Execute(commandreq.Must("delete", "Movies", "fixedid006", map[string]any{"#": "notTheRealVersion"}))
	if res["ok"] != false {
		t.Fatalf("expected delete version mismatch to fail: %#v", res)
	}
	AssertGolden(t, "delete_version_mismatch", res)
}

func TestGoldenCollectionNotFound(t *testing.T) {
	eng := newContractEngine(t)
	res := eng.Execute(commandreq.Must("create", "NoSuchCollection", "01TESTNOSUCH00000000000003", map[string]any{"$": "NoSuchCollection:0", "title": "x"}))
	if res["ok"] != false {
		t.Fatalf("expected unknown collection to fail: %#v", res)
	}
	AssertGolden(t, "create_collection_not_found", res)
}

// TestGoldenWrongMachine exercises SHARDING.md's wrongMachine response
// shape: a server that is not the shard's SHARD_SOT_MEMBER refuses
// create/patch/delete without routing hints. Clients must refresh
// establishment config and recompute the next hop locally.
func TestGoldenWrongMachine(t *testing.T) {
	eng := newContractEngine(t)
	eng.ServerName = "notTheSOTMember"
	// A fixed document ID keeps the resulting shard slot (a CRC32 hash of
	// the ID) stable across runs, unlike the auto-generated ULID "null"
	// would produce.
	res := eng.Execute(commandreq.Must("create", "Movies", "fixedid007", map[string]any{"$": "Movies:0", "title": "The Matrix"}))
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
	res := eng.Execute(commandreq.Must("frobnicate", "Movies", "null", map[string]any{}))
	if res["ok"] != false {
		t.Fatalf("expected unknown command to fail: %#v", res)
	}
	AssertGolden(t, "unknown_command", res)
}

func TestGoldenFileCreateOK(t *testing.T) {
	eng := newContractEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "fixedidfile01", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	res := eng.PutFile(strings.NewReader("abc"), "Movies", "fixedidfile01", "poster.png", engine.PutFileOptions{
		ContentType: "image/png",
		OperationID: "01FIXEDFILEOP000000000001",
	})
	if res["ok"] != true {
		t.Fatalf("expected file create to succeed: %#v", res)
	}
	AssertGolden(t, "file_create_ok", res)
}

func TestGoldenFileExists(t *testing.T) {
	eng := newContractEngine(t)
	_ = eng.Execute(commandreq.Must("create", "Movies", "fixedidfile02", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	first := eng.PutFile(strings.NewReader("abc"), "Movies", "fixedidfile02", "poster.png", engine.PutFileOptions{ContentType: "image/png"})
	if first["ok"] != true {
		t.Fatalf("expected first put to succeed: %#v", first)
	}
	res := eng.PutFile(strings.NewReader("abc"), "Movies", "fixedidfile02", "poster.png", engine.PutFileOptions{ContentType: "image/png"})
	if res["ok"] != false {
		t.Fatalf("expected fileExists: %#v", res)
	}
	AssertGolden(t, "file_exists", res)
}

func TestGoldenFileListOK(t *testing.T) {
	eng := newContractEngine(t)
	_ = eng.Execute(commandreq.Must("create", "Movies", "fixedidfile03", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	put := eng.PutFile(strings.NewReader("abc"), "Movies", "fixedidfile03", "poster.png", engine.PutFileOptions{
		ContentType: "image/png",
		OperationID: "01FIXEDFILEOP000000000003",
	})
	if put["ok"] != true {
		t.Fatalf("expected put: %#v", put)
	}
	res := eng.ListFiles("Movies", "fixedidfile03")
	if res["ok"] != true {
		t.Fatalf("expected list: %#v", res)
	}
	AssertGolden(t, "file_list_ok", res)
}

func TestGoldenFileInvalidName(t *testing.T) {
	eng := newContractEngine(t)
	_ = eng.Execute(commandreq.Must("create", "Movies", "fixedidfile04", map[string]any{"$": "Movies:0", "title": "Arrival"}))
	res := eng.PutFile(strings.NewReader("x"), "Movies", "fixedidfile04", ".hidden", engine.PutFileOptions{})
	if res["ok"] != false {
		t.Fatalf("expected invalidFileName: %#v", res)
	}
	AssertGolden(t, "file_invalid_name", res)
}
