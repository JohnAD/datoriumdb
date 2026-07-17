//go:build crash

package crash

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestSIGKILLDuringConcurrentWritesPreservesDurability fires a burst of
// concurrent create commands at a real subprocess, SIGKILLs it partway
// through, restarts it, and verifies:
//  1. every document file on disk is valid, complete JSON (no partial
//     writes survive a kill -9, because WriteFileAtomic only ever renames
//     a fully-written, fsynced temp file into place), and
//  2. every document that the server had already returned ok:true for
//     before the kill is still present and readable after restart.
func TestSIGKILLDuringConcurrentWritesPreservesDurability(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	configDir := testutil.TempConfigDir(t)
	dataDir := testutil.TempDataDir(t)

	srv := testutil.StartServer(t, testutil.ServerOptions{
		BinPath:          bin,
		ServerName:       "serverA",
		EstablishmentURL: "http://ignored.invalid",
		ConfigDir:        configDir,
		DataDir:          dataDir,
	})

	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "crash-test-client")
	ctx := context.Background()

	const writers = 8
	const perWriter = 40
	var mu sync.Mutex
	confirmedIDs := map[string]bool{}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				select {
				case <-stop:
					return
				default:
				}
				title := fmt.Sprintf("Writer %d Doc %d", w, i)
				res, err := testutil.PostCommand(ctx, srv.BaseURL, token, fmt.Sprintf(`create Movies null {$: Movies:0, title: %q}`, title))
				if err != nil {
					return // server likely just got killed
				}
				if res["ok"] == true {
					if id, ok := res["id"].(string); ok {
						mu.Lock()
						confirmedIDs[id] = true
						mu.Unlock()
					}
				}
			}
		}(w)
	}

	// Let the workload run briefly, then hard-kill mid-flight.
	time.Sleep(150 * time.Millisecond)
	srv.Kill(t)
	close(stop)
	wg.Wait()

	mu.Lock()
	idsBeforeKill := make([]string, 0, len(confirmedIDs))
	for id := range confirmedIDs {
		idsBeforeKill = append(idsBeforeKill, id)
	}
	mu.Unlock()
	if len(idsBeforeKill) == 0 {
		t.Fatalf("expected at least one confirmed create before the kill")
	}

	// Every document file on disk must be syntactically valid JSON: an
	// atomic-rename write is never torn by a kill -9.
	assertAllDocumentsAreValidJSON(t, filepath.Join(dataDir, "Movies"))

	// Restart against the same config/data dirs and confirm every
	// previously confirmed document is still readable.
	srv.Restart(t)
	for _, id := range idsBeforeKill {
		res, err := testutil.PostCommand(ctx, srv.BaseURL, token, `read Movies `+id+` {}`)
		if err != nil {
			t.Fatalf("read %s after restart: %v", id, err)
		}
		if res["ok"] != true {
			t.Fatalf("expected previously confirmed document %s to survive a hard kill: %#v", id, res)
		}
	}
}

// TestKilledReadMemberCatchesUpAfterRestart kills the read-member outright
// (not a graceful stop), writes to the SOT-member while it is down
// (producing a durable pending write per REPLICATION-FAILURE-HANDLING.md),
// restarts the read-member, and confirms its catch-up loop applies the
// pending write and clears it without operator intervention.
func TestKilledReadMemberCatchesUpAfterRestart(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	topo := newTwoNodeTopology(t)

	topo.StartA(t, bin)
	srvB := topo.StartB(t, bin)

	cfg, err := config.Load(topo.ConfigDirA)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "crash-test-client")
	ctx := context.Background()

	// Hard-kill the read-member while it is otherwise healthy.
	srvB.Kill(t)

	created, err := testutil.PostCommand(ctx, "http://"+topo.ServerAAddr, token, `create Movies null {$: Movies:0, title: "Read Member Was Down"}`)
	if err != nil {
		t.Fatalf("create while read-member is down: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected SOT-local success even with the read-member down: %#v", created)
	}
	note, hasNote := created["note"].(map[string]any)
	if !hasNote {
		t.Fatalf("expected a note describing incomplete replication: %#v", created)
	}
	// note came back through a real HTTP JSON round trip, so array fields
	// decode as []any rather than []string.
	var unacked []string
	if raw, ok := note["unacknowledged"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				unacked = append(unacked, s)
			}
		}
	}
	if len(unacked) != 1 || unacked[0] != "serverB" {
		t.Fatalf("expected serverB unacknowledged: %#v", note)
	}
	id, _ := created["id"].(string)

	// Restart the read-member; its catch-up loop (readMemberCheckinSeconds
	// is set to 1 in this topology's fixture) should apply the pending
	// write without any external repair action.
	srvB.Restart(t)

	testutil.PollUntilErr(t, 10*time.Second, 100*time.Millisecond, func() error {
		res, err := testutil.PostCommand(ctx, srvB.BaseURL, token, `read Movies `+id+` {}`)
		if err != nil {
			return err
		}
		if res["ok"] != true {
			return fmt.Errorf("read not yet ok: %#v", res)
		}
		return nil
	})
}

// assertAllDocumentsAreValidJSON walks a collection directory and fails
// the test if any *.json file fails to parse, which would indicate a
// torn/partial write surviving a crash.
func assertAllDocumentsAreValidJSON(t *testing.T, collectionDir string) {
	t.Helper()
	entries, err := os.ReadDir(collectionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read collection dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(collectionDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("document %s is not valid JSON after a hard kill (torn write): %v", path, err)
		}
		checked++
	}
	if checked == 0 {
		t.Fatalf("expected at least one document file to check in %s", collectionDir)
	}
}
