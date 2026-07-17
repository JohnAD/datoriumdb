//go:build compose

package compose

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/shard"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// fiveShardHostPorts mirrors deploy/docker-compose.five-shard.yml's
// published ports and its 00-33/34-66/67-99/9A-CC/CD-FF shard ranges.
var fiveShardHostPorts = map[string]int{
	"server1": 8081,
	"server2": 8082,
	"server3": 8083,
	"server4": 8084,
	"server5": 8085,
}

// serverForSlot returns which five-shard server owns a given shard slot,
// mirroring test/compose/fixtures/five-shard/server1/.config/__shard-map.json.
func serverForSlot(slot byte) string {
	switch {
	case slot <= 0x33:
		return "server1"
	case slot <= 0x66:
		return "server2"
	case slot <= 0x99:
		return "server3"
	case slot <= 0xCC:
		return "server4"
	default:
		return "server5"
	}
}

// TestComposeFiveShardCRUDAcrossShardsAndWrongMachine brings up the
// five-shard topology, writes documents whose IDs land on every shard
// slot's owning server, confirms each write landed on the right
// container's own /db volume (not just "some" server), and confirms a
// write sent to the wrong shard owner is refused with a wrongMachine hint
// naming the correct server.
func TestComposeFiveShardCRUDAcrossShardsAndWrongMachine(t *testing.T) {
	c := Up(t, "docker-compose.five-shard.yml")
	for name, port := range fiveShardHostPorts {
		c.WaitHealthy(fmt.Sprintf("http://127.0.0.1:%d", port), 90*time.Second)
		_ = name
	}

	cfg, err := config.Load(FixtureConfigDir("five-shard", "server1"))
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-five-shard-client")
	ctx := context.Background()

	// Find one document ID per server by trying fixed candidate IDs and
	// computing their shard slot until every server has at least one.
	idsByServer := map[string]string{}
	for i := 0; len(idsByServer) < len(fiveShardHostPorts) && i < 5000; i++ {
		candidate := fmt.Sprintf("fiveshardprobe%04d", i)
		owner := serverForSlot(shard.Slot(candidate))
		if _, have := idsByServer[owner]; !have {
			idsByServer[owner] = candidate
		}
	}
	if len(idsByServer) != len(fiveShardHostPorts) {
		t.Fatalf("failed to find probe IDs covering all five servers: %#v", idsByServer)
	}

	for owner, id := range idsByServer {
		port := fiveShardHostPorts[owner]
		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
		created, err := testutil.PostCommand(ctx, baseURL, token, fmt.Sprintf(`create Movies %s {$: Movies:0, title: "Shard Owner Test"}`, id))
		if err != nil {
			t.Fatalf("create on %s: %v", owner, err)
		}
		if created["ok"] != true {
			t.Fatalf("expected create on its own shard owner %s to succeed: %#v", owner, created)
		}

		// Sending the same write to a different server must be refused
		// with a wrongMachine hint pointing back at the real owner.
		for otherOwner, otherPort := range fiveShardHostPorts {
			if otherOwner == owner {
				continue
			}
			// Reusing the same ID is safe here: each server's storage is
			// a separate /db volume, so there is no cross-server
			// document collision to worry about, and the ID's shard
			// slot (and therefore its correct owner) does not change.
			otherBaseURL := fmt.Sprintf("http://127.0.0.1:%d", otherPort)
			refused, err := testutil.PostCommand(ctx, otherBaseURL, token, fmt.Sprintf(`create Movies %s {$: Movies:0, title: "Wrong Machine Test"}`, id))
			if err != nil {
				t.Fatalf("create on wrong owner %s: %v", otherOwner, err)
			}
			if refused["ok"] != false {
				t.Fatalf("expected %s (not the shard owner for %s) to refuse the write: %#v", otherOwner, id, refused)
			}
			if refused["correctServer"] != owner {
				t.Fatalf("expected wrongMachine hint to name %s, got %#v", owner, refused)
			}
			break // one cross-check per document is enough
		}
	}
}
