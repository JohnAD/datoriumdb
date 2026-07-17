//go:build compose

// Package compose drives the docker-compose topologies under deploy/ as
// real multi-container clusters and exercises them over their published
// host ports. These are the heaviest, slowest tests in the repository and
// require a working Docker (or Docker-Compose-compatible) daemon; they
// skip gracefully via t.Skip when one is not available, per the "compose
// tests must skip gracefully if docker unavailable" constraint.
//
// Build and run with `go test -tags compose ./test/compose/...`. Each
// test uses its own Compose project name (derived from the topology file
// name) so parallel runs and CI don't collide, and always runs `down -v`
// in a t.Cleanup to avoid leaking containers/volumes between runs.
package compose

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/test/testutil"
)

var (
	dockerCheckOnce sync.Once
	dockerAvailable bool
	dockerCheckErr  error
)

// DeployDir returns the absolute path to deploy/, where the Compose
// topology files live.
func DeployDir() string {
	return filepath.Join(testutil.RepoRoot(), "deploy")
}

// FixtureConfigDir returns the absolute path to one topology's
// establishment-server config fixture, e.g.
// FixtureConfigDir("single-node", "serverA") ->
// {repo}/test/compose/fixtures/single-node/serverA/.config. Tests use
// this to load the same auth trust material the running container was
// seeded with, so they can mint matching client tokens without a machine
// token bootstrap round trip.
func FixtureConfigDir(topology, server string) string {
	return filepath.Join(testutil.RepoRoot(), "test", "compose", "fixtures", topology, server, ".config")
}

// RequireDocker skips the test (with an explanatory message) unless a
// working `docker compose` is available. It checks once per test binary
// run and caches the result.
func RequireDocker(t *testing.T) {
	t.Helper()
	dockerCheckOnce.Do(func() {
		dockerAvailable, dockerCheckErr = probeDocker()
	})
	if !dockerAvailable {
		t.Skipf("skipping compose test: docker is not usable in this environment: %v", dockerCheckErr)
	}
}

func probeDocker() (bool, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return false, fmt.Errorf("docker CLI not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// `docker info` requires a reachable, authorized daemon connection,
	// unlike `docker --version` which only checks the CLI binary.
	cmd := exec.CommandContext(ctx, "docker", "info")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker daemon not reachable: %w: %s", err, strings.TrimSpace(out.String()))
	}
	cmd = exec.CommandContext(ctx, "docker", "compose", "version")
	out.Reset()
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker compose plugin not usable: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return true, nil
}

// Cluster wraps one running Compose project.
type Cluster struct {
	t       *testing.T
	file    string
	project string
}

// Up brings up the named topology file (relative to deploy/, e.g.
// "docker-compose.single-node.yml") under a unique project name, building
// images as needed, and registers a t.Cleanup that tears it down
// (including volumes) so state never leaks between test runs.
func Up(t *testing.T, file string, extraArgs ...string) *Cluster {
	t.Helper()
	RequireDocker(t)
	project := fmt.Sprintf("datoriumdb-%s-%d", strings.TrimSuffix(strings.TrimPrefix(file, "docker-compose."), ".yml"), time.Now().UnixNano())
	c := &Cluster{t: t, file: file, project: project}

	args := append([]string{"compose", "-f", file, "-p", project}, extraArgs...)
	args = append(args, "up", "--build", "-d", "--wait", "--wait-timeout", "180")
	if out, err := c.run(args...); err != nil {
		c.dumpLogs()
		t.Fatalf("docker compose up failed: %v\n%s", err, out)
	}
	// On failure, leave the containers/volumes running so a CI step can
	// collect container logs and data-tree snapshots before the runner
	// is torn down (see .github/workflows/ci.yml's "dump compose logs
	// and data trees on failure" step). Successful runs always clean up
	// immediately so repeated local runs don't accumulate containers.
	t.Cleanup(func() {
		if t.Failed() {
			c.t.Logf("leaving compose project %q up for post-mortem inspection (docker compose -f %s -p %s ...)", c.project, c.file, c.project)
			return
		}
		c.Down()
	})
	return c
}

// Down tears down the cluster and removes its volumes. Safe to call
// multiple times.
func (c *Cluster) Down() {
	_, _ = c.run("compose", "-f", c.file, "-p", c.project, "down", "-v", "--remove-orphans")
}

// Exec runs `docker compose ... run/exec`-style subcommands against this
// project, e.g. c.RunOneOff("run", "--rm", "serverB-bad-secret").
func (c *Cluster) run(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Dir = DeployDir()
	cmd.Env = append(os.Environ(), "COMPOSE_HTTP_TIMEOUT=180")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Run is a public wrapper around one `docker <args...>` invocation scoped
// to this project's compose file, for scenario-specific commands like
// `--profile bad-secret up serverB-bad-secret` or `stop serverB`.
func (c *Cluster) Run(args ...string) (string, error) {
	full := append([]string{"compose", "-f", c.file, "-p", c.project}, args...)
	return c.run(full...)
}

// Logs returns combined logs for the whole project (or specific services
// if names are given), for on-failure diagnostics.
func (c *Cluster) Logs(services ...string) string {
	args := append([]string{"compose", "-f", c.file, "-p", c.project, "logs", "--no-color"}, services...)
	out, _ := c.run(args...)
	return out
}

func (c *Cluster) dumpLogs() {
	c.t.Logf("compose logs for project %s:\n%s", c.project, c.Logs())
}

// WaitHealthy polls baseURL's /datoriumdb/v1/health until it reports
// ok:true, failing the test (with compose logs attached) if it never
// does.
func (c *Cluster) WaitHealthy(baseURL string, timeout time.Duration) {
	c.t.Helper()
	client := testutil.HTTPClient()
	ok := false
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/datoriumdb/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ok {
		c.dumpLogs()
		c.t.Fatalf("service at %s never became healthy within %s", baseURL, timeout)
	}
}
