package testutil

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
)

// DevSigningKeyPath returns the path to the checked-in TEST-ONLY Ed25519
// signing key that matches testdata/sample-config's "dev-primary" active
// key. Every fixture/topology that reuses testdata/sample-config's
// __auth.json can use this key to issue matching tokens.
func DevSigningKeyPath() string {
	return filepath.Join(SampleConfigDir(), "dev-signing-key.pem")
}

// DevRetiredSigningKeyPath returns the path to the TEST-ONLY key matching
// the "dev-old" retired key, for grace-period tests.
func DevRetiredSigningKeyPath() string {
	return filepath.Join(SampleConfigDir(), "dev-retired-signing-key.pem")
}

// NewDevIssuer builds an *auth.Issuer bound to the sample-config auth trust
// material and the checked-in dev-signing-key.pem, so tests can mint
// deterministic client/machine tokens without a running server.
func NewDevIssuer(t testing.TB, cfg *config.Config) *auth.Issuer {
	t.Helper()
	issuer, err := auth.NewIssuerFromFile(cfg.Auth, DevSigningKeyPath())
	if err != nil {
		t.Fatalf("build dev issuer: %v", err)
	}
	return issuer
}

// MachineToken issues a short-lived machine token for serverName using the
// dev signing key, for tests that need a bearer token without going
// through the HTTP bootstrap flow.
func MachineToken(t testing.TB, cfg *config.Config, serverName string) string {
	t.Helper()
	issuer := NewDevIssuer(t, cfg)
	tok, _, err := issuer.IssueMachineToken(serverName, time.Hour)
	if err != nil {
		t.Fatalf("issue machine token: %v", err)
	}
	return tok
}

// ClientToken issues a short-lived client token for subject using the dev
// signing key.
func ClientToken(t testing.TB, cfg *config.Config, subject string) string {
	t.Helper()
	issuer := NewDevIssuer(t, cfg)
	tok, _, err := issuer.IssueClientToken(subject, time.Hour)
	if err != nil {
		t.Fatalf("issue client token: %v", err)
	}
	return tok
}

// DeterministicClock returns a Clock-like function (time.Now-compatible)
// that advances by 1ms per call from a fixed start, useful for producing
// stable, reproducible ULIDs in golden/contract tests. See
// internal/idgen.SetClock.
func DeterministicClock(start time.Time) func() time.Time {
	n := 0
	return func() time.Time {
		t := start.Add(time.Duration(n) * time.Millisecond)
		n++
		return t
	}
}
