// Package testutil provides reusable helpers for DatoriumDB's higher-level
// test suites: test/contract, test/integration, test/crash, and
// test/compose. Unit tests inside internal/* packages should keep using
// their own local helpers (t.TempDir, httptest.Server, etc); this package
// exists for tests that need real subprocesses, real ports, or repeated
// filesystem/HTTP assertions across process boundaries.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// RepoRoot returns the absolute path to the repository root, computed from
// this file's own location so it works regardless of the caller's working
// directory (go test runs with cwd set to the test's package directory).
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// this file lives at {root}/test/testutil/testutil.go
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// SampleConfigDir returns the absolute path to testdata/sample-config.
func SampleConfigDir() string {
	return filepath.Join(RepoRoot(), "testdata", "sample-config")
}

// TempConfigDir copies testdata/sample-config into a fresh temp directory
// so tests can mutate config files without touching the checked-in fixture.
func TempConfigDir(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".config")
	if err := CopyDir(SampleConfigDir(), dir); err != nil {
		t.Fatalf("copy sample config: %v", err)
	}
	return dir
}

// TempDataDir returns a fresh temp directory suitable for use as a
// server's --data-dir.
func TempDataDir(t testing.TB) string {
	t.Helper()
	return t.TempDir()
}

// CopyDir recursively copies src to dst, creating dst and any parents.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// WriteFile writes contents to path, creating parent directories as needed.
// It fails the test on error, mirroring the pattern used by the existing
// internal/server integration tests.
func WriteFile(t testing.TB, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// WriteJSONFile marshals v as indented JSON and writes it to path.
func WriteJSONFile(t testing.TB, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	WriteFile(t, path, string(data)+"\n")
}

// FreePort asks the OS for a free TCP port on 127.0.0.1 and returns it.
// There is an inherent TOCTOU race (the port could be taken before the
// caller binds it), but it is good enough for test harnesses that
// immediately spawn a listener.
func FreePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// FreeAddr is a convenience wrapper around FreePort returning a full
// "127.0.0.1:PORT" listen address.
func FreeAddr(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("127.0.0.1:%d", FreePort(t))
}

// PollUntil polls cond every interval until it returns true or timeout
// elapses, in which case it fails the test with msg (formatted with args
// like t.Fatalf).
func PollUntil(t testing.TB, timeout, interval time.Duration, cond func() bool, msg string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(msg, args...)
		}
		time.Sleep(interval)
	}
}

// PollUntilErr is like PollUntil but for conditions that also report why
// they have not yet succeeded, which is included in the failure message.
func PollUntilErr(t testing.TB, timeout, interval time.Duration, cond func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := cond(); err == nil {
			return
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition never became true within %s: %v", timeout, lastErr)
		}
		time.Sleep(interval)
	}
}

// HTTPClient returns an *http.Client with a bounded timeout suitable for
// talking to local test servers.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// WaitForHealth polls GET {baseURL}/datoriumdb/v1/health until it returns
// HTTP 200 with ok:true, or fails the test after timeout.
func WaitForHealth(t testing.TB, baseURL string, timeout time.Duration) {
	t.Helper()
	client := HTTPClient()
	PollUntilErr(t, timeout, 100*time.Millisecond, func() error {
		resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/datoriumdb/v1/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		if out["ok"] != true {
			return fmt.Errorf("health not ok: %#v", out)
		}
		return nil
	})
}

// PostCommand sends an access-language command to POST /datoriumdb/v1/command
// on baseURL, authenticated with bearerToken (may be empty), and returns the
// decoded envelope.
func PostCommand(ctx context.Context, baseURL, bearerToken, command string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/datoriumdb/v1/command", strings.NewReader(command))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	return out, nil
}

// GetJSON issues a GET request against url with an optional bearer token
// and decodes the JSON response body.
func GetJSON(ctx context.Context, url, bearerToken string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := HTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return out, resp.StatusCode, nil
}

// PostJSON issues a POST request with a JSON body and decodes a JSON
// response.
func PostJSON(ctx context.Context, url, bearerToken string, body any) (map[string]any, int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := HTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return out, resp.StatusCode, nil
}

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AssertFileExists fails the test if path does not exist.
func AssertFileExists(t testing.TB, path string) {
	t.Helper()
	if !FileExists(path) {
		t.Fatalf("expected file to exist: %s", path)
	}
}

// AssertFileMissing fails the test if path exists.
func AssertFileMissing(t testing.TB, path string) {
	t.Helper()
	if FileExists(path) {
		t.Fatalf("expected file to be absent: %s", path)
	}
}

// ReadJSONFile reads and decodes a JSON document from path.
func ReadJSONFile(t testing.TB, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}
