package replication

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
)

func testShardMapCfg() *config.Config {
	return &config.Config{
		ShardMap: config.ShardMapFile{
			ShardMap: config.ShardMapBody{
				Default: map[string]config.ShardAssignment{
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
				},
			},
		},
	}
}

func TestRelevantSOTServersForReadMember(t *testing.T) {
	cfg := testShardMapCfg()
	got := RelevantSOTServers(cfg, "serverB")
	if len(got) != 1 || got[0] != "serverA" {
		t.Fatalf("expected serverB to depend only on serverA, got %#v", got)
	}
}

func TestRelevantSOTServersForProxyMemberAcrossMultipleSlots(t *testing.T) {
	cfg := testShardMapCfg()
	got := RelevantSOTServers(cfg, "analysisA")
	want := map[string]bool{"serverA": true, "serverC": true}
	if len(got) != len(want) {
		t.Fatalf("expected analysisA to depend on both SOT members, got %#v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected SOT server %q in %#v", s, got)
		}
	}
}

func TestRelevantSOTServersExcludesSelfSOT(t *testing.T) {
	cfg := testShardMapCfg()
	got := RelevantSOTServers(cfg, "serverA")
	if len(got) != 0 {
		t.Fatalf("expected an SOT-member to have no relevant SOT dependency for its own slots, got %#v", got)
	}
}

func TestAssignmentForSlot(t *testing.T) {
	cfg := testShardMapCfg()
	low := AssignmentForSlot(cfg, 0x10)
	if low.ShardSOTMember != "serverA" {
		t.Fatalf("expected serverA to own low slot, got %#v", low)
	}
	high := AssignmentForSlot(cfg, 0xF0)
	if high.ShardSOTMember != "serverC" {
		t.Fatalf("expected serverC to own high slot, got %#v", high)
	}
}
