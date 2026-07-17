//go:build crash

// Package crash exercises hard-failure scenarios against real
// `datoriumdb` subprocesses: SIGKILL mid-workload, and a read-member that
// is killed and later restarted, proving the filesystem-backed durability
// model (WriteFileAtomic + durable per-operation state,
// tech-docs/FILESTYSTEM-STORAGE.md and REPLICATION-FAILURE-HANDLING.md)
// survives hard process termination without corruption or data loss.
// Build with `go test -tags crash ./test/crash/...`.
package crash

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/test/testutil"
)

const sharedBootstrapSecret = "crash-test-shared-secret"

// twoNodeTopology mirrors test/integration's, duplicated locally (rather
// than shared) so this package's build tag stays independent and does not
// need to import a tag-gated sibling package.
type twoNodeTopology struct {
	ServerAAddr string
	ServerBAddr string
	ConfigDirA  string
	ConfigDirB  string
	DataDirA    string
	DataDirB    string
	Bootstrap   string
}

// newTwoNodeTopology builds a serverA(SOT)/serverB(read) topology with a
// fast read-member check-in interval so post-restart catch-up (which polls
// on a timer, see cmd/datoriumdb's runCatchUpLoop) completes quickly
// enough for a test.
func newTwoNodeTopology(t *testing.T) *twoNodeTopology {
	t.Helper()
	addrA := testutil.FreeAddr(t)
	addrB := testutil.FreeAddr(t)

	configDirA := filepath.Join(t.TempDir(), "serverA", ".config")
	if err := testutil.CopyDir(testutil.SampleConfigDir(), configDirA); err != nil {
		t.Fatalf("copy sample config: %v", err)
	}
	testutil.WriteFile(t, filepath.Join(configDirA, "__servers.json"), fmt.Sprintf(`{
		"servers": {
			"serverA": {"baseURL": "http://%s"},
			"serverB": {"baseURL": "http://%s"}
		}
	}`, addrA, addrB))
	testutil.WriteFile(t, filepath.Join(configDirA, "__shard-map.json"), `{
		"shardMap": {"default": {"00-FF": {
			"SHARD_SOT_MEMBER": "serverA",
			"SHARD_READ_MEMBER": ["serverB"],
			"PROXY_READ_MEMBER": []
		}}}
	}`)
	testutil.WriteFile(t, filepath.Join(configDirA, "__general.json"), `{
		"general": {
			"name": "DatoriumDB Crash Test",
			"establishmentServer": "serverA",
			"version": 1,
			"readMemberCheckinSeconds": 1,
			"cacheUpdateCheckinSeconds": 60,
			"readMemberFailedCheckinsBeforeStale": 3
		}
	}`)

	return &twoNodeTopology{
		ServerAAddr: addrA,
		ServerBAddr: addrB,
		ConfigDirA:  configDirA,
		ConfigDirB:  filepath.Join(t.TempDir(), "serverB", ".config"),
		DataDirA:    filepath.Join(t.TempDir(), "serverA", "data"),
		DataDirB:    filepath.Join(t.TempDir(), "serverB", "data"),
		Bootstrap:   sharedBootstrapSecret,
	}
}

func (top *twoNodeTopology) StartA(t *testing.T, bin string) *testutil.ServerProcess {
	t.Helper()
	return testutil.StartServer(t, testutil.ServerOptions{
		BinPath:          bin,
		ServerName:       "serverA",
		EstablishmentURL: "http://" + top.ServerAAddr,
		Listen:           top.ServerAAddr,
		ConfigDir:        top.ConfigDirA,
		DataDir:          top.DataDirA,
		BootstrapSecret:  top.Bootstrap,
		SigningKeyFile:   testutil.DevSigningKeyPath(),
	})
}

func (top *twoNodeTopology) StartB(t *testing.T, bin string) *testutil.ServerProcess {
	t.Helper()
	return testutil.StartServer(t, testutil.ServerOptions{
		BinPath:          bin,
		ServerName:       "serverB",
		EstablishmentURL: "http://" + top.ServerAAddr,
		Listen:           top.ServerBAddr,
		ConfigDir:        top.ConfigDirB,
		DataDir:          top.DataDirB,
		BootstrapSecret:  top.Bootstrap,
	})
}
