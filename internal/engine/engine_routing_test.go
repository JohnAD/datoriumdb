package engine

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/commandreq"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/shard"
)

// idInSlotRange deterministically finds a document ID whose shard slot
// falls within [low, high], so routing tests don't depend on hard-coded
// IDs tied to the current CRC32 implementation.
func idInSlotRange(t *testing.T, low, high byte) string {
	t.Helper()
	for i := 0; i < 100000; i++ {
		id := fmt.Sprintf("ROUTEDOC%06d", i)
		s := shard.Slot(id)
		if s >= low && s <= high {
			return id
		}
	}
	t.Fatalf("could not find a document id in shard slot range [%02X-%02X]", low, high)
	return ""
}

// multiServerEngine builds an Engine whose config has two shard slot
// ranges split across four distinct servers plus one proxy-only server,
// per tech-docs/SHARDING.md and tech-docs/ESTABLISHMENT-CONFIG.md's
// example shard map. serverName sets which of those servers this Engine
// instance is acting as.
func multiServerEngine(t *testing.T, serverName string) (*Engine, byte, byte) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir("../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	eng := &Engine{ConfigDir: configDir, DataDir: root, ServerName: serverName}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	eng.Cfg.Servers.Servers = map[string]config.ServerEntry{
		"serverA":   {BaseURL: "http://127.0.0.1:9001"},
		"serverB":   {BaseURL: "http://127.0.0.1:9002"},
		"serverC":   {BaseURL: "http://127.0.0.1:9003"},
		"serverD":   {BaseURL: "http://127.0.0.1:9004"},
		"analysisA": {BaseURL: "http://127.0.0.1:9005"},
	}
	eng.Cfg.ShardMap.ShardMap.Default = map[string]config.ShardAssignment{
		"00-7F": {
			ShardSOTMember:  "serverA",
			ShardReadMember: []string{"serverB"},
			ProxyReadMember: []string{"analysisA"},
		},
		"80-FF": {
			ShardSOTMember:  "serverC",
			ShardReadMember: []string{"serverD"},
			ProxyReadMember: []string{"analysisA"},
		},
	}
	return eng, 0x00, 0x7F
}

func TestWriteRoutesToSOTOnly(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverA")
	lowID := idInSlotRange(t, low, high)
	highID := idInSlotRange(t, high+1, 0xFF)

	// serverA is the SOT for the low range: create succeeds.
	res := eng.Execute(commandreq.Must("create", "Movies", lowID, map[string]any{"$": "Movies:0", "title": "x"}))
	if res["ok"] != true {
		t.Fatalf("expected SOT create to succeed: %#v", res)
	}

	// serverA is not the SOT for the high range: create is refused.
	res = eng.Execute(commandreq.Must("create", "Movies", highID, map[string]any{"$": "Movies:0", "title": "x"}))
	assertWrongMachine(t, res, "create", "Movies", highID)
}

func TestReadRoutesToReadMemberOnly(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverA")
	lowID := idInSlotRange(t, low, high)

	// serverA is the SOT, but only serverB is SHARD_READ_MEMBER for the
	// low range: a read on serverA is refused even though it holds the
	// source-of-truth copy.
	res := eng.Execute(commandreq.Must("read", "Movies", lowID, map[string]any{}))
	assertWrongMachine(t, res, "read", "Movies", lowID)
}

func TestReadSucceedsOnAssignedReadMember(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverB")
	lowID := idInSlotRange(t, low, high)

	// serverB is a normal SHARD_READ_MEMBER: routing lets the read
	// through (it fails downstream with documentNotFound, not
	// wrongMachine, because nothing was ever replicated to it in this
	// unit test).
	res := eng.Execute(commandreq.Must("read", "Movies", lowID, map[string]any{}))
	if res["ok"] != false {
		t.Fatalf("expected read to fail (no document), got ok:true: %#v", res)
	}
	if code := firstErrorCode(res); code != "documentNotFound" {
		t.Fatalf("expected documentNotFound (routing allowed the read through), got %#v", res)
	}
}

func TestProxyReadMemberIsNotANormalReadTarget(t *testing.T) {
	eng, low, high := multiServerEngine(t, "analysisA")
	lowID := idInSlotRange(t, low, high)

	// analysisA is PROXY_READ_MEMBER only for the low range, never
	// SHARD_READ_MEMBER: normal smart-client reads must still be refused.
	res := eng.Execute(commandreq.Must("read", "Movies", lowID, map[string]any{}))
	assertWrongMachine(t, res, "read", "Movies", lowID)
}

func TestWriteRefusedOnReadOnlyMember(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverB")
	lowID := idInSlotRange(t, low, high)

	res := eng.Execute(commandreq.Must("create", "Movies", lowID, map[string]any{"$": "Movies:0", "title": "x"}))
	assertWrongMachine(t, res, "create", "Movies", lowID)
}

func TestDualRoleServerServesBothReadsAndWrites(t *testing.T) {
	// A single-machine deployment (testdata/sample-config: serverA is
	// both SHARD_SOT_MEMBER and SHARD_READ_MEMBER for 00-FF) must not
	// need any wrongMachine bounce for either command.
	eng := testEngine(t)
	created := eng.Execute(commandreq.Must("create", "Movies", "01TESTMOVIES00000000000001", map[string]any{"$": "Movies:0", "title": "x"}))
	if created["ok"] != true {
		t.Fatalf("expected dual-role create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)
	read := eng.Execute(commandreq.Must("read", "Movies", id, map[string]any{}))
	if read["ok"] != true {
		t.Fatalf("expected dual-role read to succeed: %#v", read)
	}
}

func assertWrongMachine(t *testing.T, res map[string]any, command, collection, id string) {
	t.Helper()
	if res["ok"] != false {
		t.Fatalf("expected ok:false wrongMachine, got %#v", res)
	}
	if code := firstErrorCode(res); code != "wrongMachine" {
		t.Fatalf("expected wrongMachine error code, got %q: %#v", code, res)
	}
	if res["command"] != command {
		t.Fatalf("expected flat command field %q, got %#v", command, res)
	}
	if res["collection"] != collection {
		t.Fatalf("expected flat collection field %q, got %#v", collection, res)
	}
	if res["id"] != id {
		t.Fatalf("expected flat id field %q, got %#v", id, res)
	}
	for _, banned := range []string{"correctServer", "baseURL", "shardSlot"} {
		if _, ok := res[banned]; ok {
			t.Fatalf("wrongMachine must not include routing hint field %q: %#v", banned, res)
		}
	}
	if _, ok := res["configVersion"].(int); !ok {
		t.Fatalf("expected diagnostic configVersion field, got %#v", res)
	}
}
