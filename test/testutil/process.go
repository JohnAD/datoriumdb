package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	buildMu   sync.Mutex
	binaries  = map[string]string{}
	buildOnce = map[string]*sync.Once{}
)

// BuildBinary compiles the given cmd/{name} package once per test binary
// run (subsequent calls reuse the cached path) and returns the path to the
// compiled executable. Building once keeps subprocess-cluster integration
// tests fast even when many tests each spawn several server processes.
func BuildBinary(t testing.TB, name string) string {
	t.Helper()
	buildMu.Lock()
	once, ok := buildOnce[name]
	if !ok {
		once = &sync.Once{}
		buildOnce[name] = once
	}
	buildMu.Unlock()

	var buildErr error
	once.Do(func() {
		dir, err := os.MkdirTemp("", "datoriumdb-bin-"+name+"-")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, "./cmd/"+name)
		cmd.Dir = RepoRoot()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/%s: %w\n%s", name, err, stderr.String())
			return
		}
		buildMu.Lock()
		binaries[name] = out
		buildMu.Unlock()
	})
	if buildErr != nil {
		t.Fatalf("build %s: %v", name, buildErr)
	}
	buildMu.Lock()
	path, ok := binaries[name]
	buildMu.Unlock()
	if !ok {
		t.Fatalf("build %s: binary not available after build (see earlier failure)", name)
	}
	return path
}

// ServerProcess wraps one subprocess-mode `datoriumdb` (or any other
// long-running DatoriumDB-family binary) instance.
type ServerProcess struct {
	Name     string
	BaseURL  string
	Listen   string
	Args     []string
	Env      []string
	BinPath  string
	LogPath  string
	LogBuf   *syncBuffer
	cmd      *exec.Cmd
	t        testing.TB
	stopOnce sync.Once
}

// ServerOptions configures StartServer.
type ServerOptions struct {
	// BinPath is the compiled datoriumdb binary path (see BuildBinary).
	BinPath string
	// ServerName is the server's own name, per ESTABLISHMENT-CONFIG.md
	// startup parameters.
	ServerName string
	// EstablishmentURL is the establishment server base URL.
	EstablishmentURL string
	// Listen defaults to a freshly allocated 127.0.0.1:PORT.
	Listen string
	// ConfigDir defaults to a fresh temp dir.
	ConfigDir string
	// DataDir defaults to a fresh temp dir.
	DataDir string
	// BootstrapSecret sets DATORIUMDB_MACHINE_BOOTSTRAP_SECRET.
	BootstrapSecret string
	// SigningKeyFile sets DATORIUMDB_SIGNING_KEY_FILE (establishment
	// server only).
	SigningKeyFile string
	// ExtraEnv appends additional "KEY=VALUE" environment entries.
	ExtraEnv []string
}

// StartServer launches one datoriumdb server subprocess and waits for it
// to answer /datoriumdb/v1/health, per the "subprocess clusters" test
// infrastructure requirement. The process and its log file are cleaned up
// automatically via t.Cleanup.
func StartServer(t testing.TB, opts ServerOptions) *ServerProcess {
	t.Helper()
	if opts.Listen == "" {
		opts.Listen = FreeAddr(t)
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Join(t.TempDir(), opts.ServerName, ".config")
		if err := os.MkdirAll(opts.ConfigDir, 0o755); err != nil {
			t.Fatalf("mkdir config dir: %v", err)
		}
	}
	if opts.DataDir == "" {
		opts.DataDir = filepath.Join(t.TempDir(), opts.ServerName, "data")
		if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
			t.Fatalf("mkdir data dir: %v", err)
		}
	}

	args := []string{
		opts.ServerName,
		opts.EstablishmentURL,
		"--listen", opts.Listen,
		"--config-dir", opts.ConfigDir,
		"--data-dir", opts.DataDir,
	}
	env := os.Environ()
	if opts.BootstrapSecret != "" {
		env = append(env, "DATORIUMDB_MACHINE_BOOTSTRAP_SECRET="+opts.BootstrapSecret)
	}
	if opts.SigningKeyFile != "" {
		env = append(env, "DATORIUMDB_SIGNING_KEY_FILE="+opts.SigningKeyFile)
	}
	env = append(env, opts.ExtraEnv...)

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, opts.ServerName+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}

	cmd := exec.Command(opts.BinPath, args...)
	cmd.Env = env
	buf := &syncBuffer{}
	cmd.Stdout = io.MultiWriter(logFile, buf)
	cmd.Stderr = cmd.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", opts.ServerName, err)
	}

	sp := &ServerProcess{
		Name:    opts.ServerName,
		BaseURL: "http://" + opts.Listen,
		Listen:  opts.Listen,
		Args:    args,
		Env:     env,
		BinPath: opts.BinPath,
		LogPath: logPath,
		LogBuf:  buf,
		cmd:     cmd,
		t:       t,
	}
	t.Cleanup(func() {
		sp.Stop()
		logFile.Close()
	})
	WaitForHealth(t, sp.BaseURL, 15*time.Second)
	return sp
}

// Stop gracefully terminates the process (SIGTERM) and waits briefly, then
// force-kills if it hasn't exited. Safe to call multiple times.
func (p *ServerProcess) Stop() {
	p.stopOnce.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
	})
}

// Kill immediately SIGKILLs the process, simulating a hard crash. Unlike
// Stop, this can be called mid-test and the ServerProcess can then be
// restarted with Restart to exercise crash-recovery behavior.
func (p *ServerProcess) Kill(t testing.TB) {
	t.Helper()
	if p.cmd.Process == nil {
		return
	}
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill %s: %v", p.Name, err)
	}
	_ = p.cmd.Wait()
}

// Wait blocks until the process exits and returns its error (nil on a
// clean exit). Useful after Kill or Stop to assert on exit status.
func (p *ServerProcess) Wait() error {
	return p.cmd.Wait()
}

// Restart starts a brand new process with the same args/env (reusing the
// same config/data directories), simulating a crash-restart. The previous
// process must already have exited (call Kill or Stop first).
func (p *ServerProcess) Restart(t testing.TB) {
	t.Helper()
	cmd := exec.Command(p.BinPath, p.Args...)
	cmd.Env = p.Env
	cmd.Stdout = p.LogBuf
	cmd.Stderr = p.LogBuf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("restart %s: %v", p.Name, err)
	}
	p.cmd = cmd
	p.stopOnce = sync.Once{}
	t.Cleanup(p.Stop)
	WaitForHealth(t, p.BaseURL, 15*time.Second)
}

// Log returns the captured combined stdout/stderr so far.
func (p *ServerProcess) Log() string {
	return p.LogBuf.String()
}

// KillProcessGroup is a lower-level helper for crash tests that need to
// send an arbitrary signal (e.g. SIGKILL) directly, bypassing Stop's
// graceful-then-forceful sequence.
func KillProcessGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}

// syncBuffer is a concurrency-safe bytes.Buffer for capturing subprocess
// output that tests may poll from a different goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
