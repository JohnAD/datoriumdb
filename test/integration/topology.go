//go:build integration

package integration

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/test/testutil"
)

// twoNodeTopology is serverA (establishment self, SHARD_SOT_MEMBER for
// 00-FF) plus serverB (bootstraps against serverA, SHARD_READ_MEMBER for
// 00-FF), wired with real listen addresses so serverB can genuinely
// bootstrap over HTTP against a real subprocess, per
// tech-docs/ESTABLISHMENT-CONFIG.md's "Server Startup".
type twoNodeTopology struct {
	ServerAAddr string
	ServerBAddr string
	ConfigDirA  string
	ConfigDirB  string
	DataDirA    string
	DataDirB    string
	Bootstrap   string
}

const sharedBootstrapSecret = "integration-test-shared-secret"

// newTwoNodeTopology allocates ports for serverA/serverB and writes
// serverA's establishment config (a customized copy of
// testdata/sample-config) naming both servers. serverB starts with an
// empty config dir and bootstraps against serverA.
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
			"name": "DatoriumDB Integration Test",
			"establishmentServer": "serverA",
			"version": 1,
			"readMemberCheckinSeconds": 1,
			"cacheUpdateCheckinSeconds": 60,
			"readMemberFailedCheckinsBeforeStale": 3
		}
	}`)

	configDirB := filepath.Join(t.TempDir(), "serverB", ".config")

	return &twoNodeTopology{
		ServerAAddr: addrA,
		ServerBAddr: addrB,
		ConfigDirA:  configDirA,
		ConfigDirB:  configDirB,
		DataDirA:    filepath.Join(t.TempDir(), "serverA", "data"),
		DataDirB:    filepath.Join(t.TempDir(), "serverB", "data"),
		Bootstrap:   sharedBootstrapSecret,
	}
}

// StartA starts serverA (establishment self + SOT-member) and waits for
// it to become healthy.
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

// StartB starts serverB (bootstraps against serverA) and waits for it to
// become healthy (health does not require establishment to have
// succeeded, so callers should also poll /ready or a real command).
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
