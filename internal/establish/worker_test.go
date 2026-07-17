package establish

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEstablishmentServer is a minimal stand-in for the real HTTP server,
// used to unit test Worker's refresh scheduling in isolation from
// internal/server.
type fakeEstablishmentServer struct {
	establishCalls atomic.Int32
	tokenCalls     atomic.Int32
	failEstablish  atomic.Bool
}

func (f *fakeEstablishmentServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /datoriumdb/v1/auth/machine-token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenCalls.Add(1)
		writeOK(w, map[string]any{"token": "fake-token", "expiresIn": 3600})
	})
	mux.HandleFunc("GET /datoriumdb/v1/establish", func(w http.ResponseWriter, r *http.Request) {
		f.establishCalls.Add(1)
		if f.failEstablish.Load() {
			writeFail(w, "someError", "forced failure")
			return
		}
		writeOK(w, map[string]any{
			"general":  map[string]any{"name": "x", "establishmentServer": "serverA", "version": 1},
			"servers":  map[string]any{"serverA": map[string]any{"baseURL": "http://127.0.0.1:1"}},
			"shardMap": map[string]any{"default": map[string]any{}},
			"auth":     map[string]any{"issuer": "iss", "audience": "aud", "keys": []any{}},
			"schemas":  map[string]any{},
			"searches": map[string]any{},
		})
	})
	return mux
}

func writeOK(w http.ResponseWriter, fields map[string]any) {
	out := map[string]any{"ok": true}
	for k, v := range fields {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func writeFail(w http.ResponseWriter, code, message string) {
	out := map[string]any{"ok": false, "errors": []map[string]any{{"code": code, "message": message}}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func TestHasLocalConfigFalseForMissingDir(t *testing.T) {
	if HasLocalConfig(t.TempDir() + "/does-not-exist") {
		t.Fatalf("expected no local config for a nonexistent directory")
	}
}

func TestWorkerRunRefreshesImmediatelyOnWake(t *testing.T) {
	fake := &fakeEstablishmentServer{}
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	root := t.TempDir()
	w := &Worker{
		ServerName:       "serverB",
		EstablishmentURL: ts.URL,
		BootstrapSecret:  "secret",
		ConfigDir:        filepath.Join(root, ".config"),
		DataDir:          filepath.Join(root, "data"),
		RefreshInterval:  time.Hour, // long enough that the ticker never fires during the test
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Run(ctx, nil)
		close(done)
	}()

	// Give Run a moment to install its wake channel, then request an
	// immediate event-driven refresh.
	time.Sleep(20 * time.Millisecond)
	w.Wake()

	deadline := time.After(2 * time.Second)
	for fake.establishCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("expected Wake to trigger an immediate refresh")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	<-done
}

func TestWorkerRunStopsAfterMaxConsecutiveFailures(t *testing.T) {
	fake := &fakeEstablishmentServer{}
	fake.failEstablish.Store(true)
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	root := t.TempDir()
	w := &Worker{
		ServerName:             "serverB",
		EstablishmentURL:       ts.URL,
		BootstrapSecret:        "secret",
		ConfigDir:              filepath.Join(root, ".config"),
		DataDir:                filepath.Join(root, "data"),
		RefreshInterval:        5 * time.Millisecond,
		MaxConsecutiveFailures: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var fatalErr error
	fatalCh := make(chan struct{})
	go func() {
		w.Run(ctx, func(err error) {
			fatalErr = err
			close(fatalCh)
		})
	}()

	select {
	case <-fatalCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("expected onFatal to be called after repeated failures")
	}
	if fatalErr == nil {
		t.Fatalf("expected a non-nil error passed to onFatal")
	}
	if fake.establishCalls.Load() < 3 {
		t.Fatalf("expected at least 3 establish attempts, got %d", fake.establishCalls.Load())
	}
}

func TestWorkerRunStopsOnContextCancel(t *testing.T) {
	fake := &fakeEstablishmentServer{}
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	root := t.TempDir()
	w := &Worker{
		ServerName:       "serverB",
		EstablishmentURL: ts.URL,
		BootstrapSecret:  "secret",
		ConfigDir:        filepath.Join(root, ".config"),
		DataDir:          filepath.Join(root, "data"),
		RefreshInterval:  5 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx, func(error) { t.Errorf("onFatal should not be called on clean shutdown") })
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected Run to return promptly after context cancellation")
	}
}
