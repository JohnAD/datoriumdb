package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

func testAuthFile(t *testing.T, pub ed25519.PublicKey, retiredPub ed25519.PublicKey) config.AuthFile {
	t.Helper()
	keys := []config.AuthKey{
		{Kid: "dev-primary", Alg: "EdDSA", Status: "active", PublicKey: EncodePublicKey(pub)},
	}
	if retiredPub != nil {
		keys = append(keys, config.AuthKey{Kid: "dev-old", Alg: "EdDSA", Status: "retired", PublicKey: EncodePublicKey(retiredPub)})
	}
	var af config.AuthFile
	af.Auth.Issuer = "https://issuer.test"
	af.Auth.Audience = "datoriumdb"
	af.Auth.TokenLifetimeSeconds.Client = 3600
	af.Auth.TokenLifetimeSeconds.Machine = 3600
	af.Auth.Keys = keys
	return af
}

func mustKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestIssueAndValidateClientToken(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)

	issuer, err := NewIssuer(cfg, priv)
	if err != nil {
		t.Fatal(err)
	}
	token, lifetime, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if lifetime != time.Hour {
		t.Fatalf("expected default 1h lifetime, got %v", lifetime)
	}

	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := validator.ParseToken(token)
	if err != nil {
		t.Fatalf("expected valid token: %v", err)
	}
	if claims.Kind != KindClient || claims.Subject != "alice" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if claims.ServerName != "" {
		t.Fatalf("client token must not carry a server name: %#v", claims)
	}
}

func TestIssueAndValidateMachineToken(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)

	issuer, err := NewIssuer(cfg, priv)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := issuer.IssueMachineToken("serverB", 0)
	if err != nil {
		t.Fatal(err)
	}

	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := validator.ParseToken(token)
	if err != nil {
		t.Fatalf("expected valid token: %v", err)
	}
	if claims.Kind != KindMachine || claims.ServerName != "serverB" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if err := RequireMachine(claims, "serverB"); err != nil {
		t.Fatalf("expected match: %v", err)
	}
	if err := RequireMachine(claims, "serverC"); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestParseBearerMissingHeader(t *testing.T) {
	pub, _ := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validator.ParseBearer(""); errCode(err) != "unauthenticated" {
		t.Fatalf("expected unauthenticated, got %v", err)
	}
	if _, err := validator.ParseBearer("Basic abc"); errCode(err) != "unauthenticated" {
		t.Fatalf("expected unauthenticated for non-bearer scheme, got %v", err)
	}
	if _, err := validator.ParseBearer("Bearer "); errCode(err) != "unauthenticated" {
		t.Fatalf("expected unauthenticated for empty bearer, got %v", err)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	issuer, err := NewIssuer(cfg, priv)
	if err != nil {
		t.Fatal(err)
	}
	issuer.Now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	token, _, err := issuer.IssueClientToken("alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = validator.ParseToken(token)
	if errCode(err) != "tokenExpired" {
		t.Fatalf("expected tokenExpired, got %v", err)
	}
}

func TestWrongIssuerAndAudienceRejected(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}

	wrongIssuer := craftToken(t, priv, "dev-primary", "https://someone-else.test", "datoriumdb", KindClient, "alice", "", time.Hour)
	if errCode2(validator.ParseToken(wrongIssuer)) != "invalidToken" {
		t.Fatalf("expected invalidToken for wrong issuer")
	}

	wrongAudience := craftToken(t, priv, "dev-primary", cfg.Auth.Issuer, "someone-else", KindClient, "alice", "", time.Hour)
	if errCode2(validator.ParseToken(wrongAudience)) != "invalidToken" {
		t.Fatalf("expected invalidToken for wrong audience")
	}
}

func TestUnknownKidRejected(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token := craftToken(t, priv, "unknown-kid", cfg.Auth.Issuer, cfg.Auth.Audience, KindClient, "alice", "", time.Hour)
	if errCode2(validator.ParseToken(token)) != "invalidToken" {
		t.Fatalf("expected invalidToken for unknown kid")
	}
}

func TestMissingOrUnknownKindRejected(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token := craftTokenRaw(t, priv, "dev-primary", cfg.Auth.Issuer, cfg.Auth.Audience, "alice", time.Hour, nil)
	if errCode2(validator.ParseToken(token)) != "invalidToken" {
		t.Fatalf("expected invalidToken for missing kind claim")
	}

	claims := map[string]any{ClaimKind: "root"}
	token2 := craftTokenRaw(t, priv, "dev-primary", cfg.Auth.Issuer, cfg.Auth.Audience, "alice", time.Hour, claims)
	if errCode2(validator.ParseToken(token2)) != "invalidToken" {
		t.Fatalf("expected invalidToken for unknown kind claim")
	}
}

func TestMachineTokenMissingServerNameRejected(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{ClaimKind: string(KindMachine)}
	token := craftTokenRaw(t, priv, "dev-primary", cfg.Auth.Issuer, cfg.Auth.Audience, "serverB", time.Hour, claims)
	if errCode2(validator.ParseToken(token)) != "invalidToken" {
		t.Fatalf("expected invalidToken for missing serverName claim")
	}
}

func TestClientTokenRequiredMachineForSys(t *testing.T) {
	pub, priv := mustKeyPair(t)
	cfg := testAuthFile(t, pub, nil)
	issuer, err := NewIssuer(cfg, priv)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := validator.ParseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireMachine(claims, ""); errCode(err) != "machineIdentityMismatch" {
		t.Fatalf("expected machineIdentityMismatch, got %v", err)
	}
}

func TestRetiredKeyGracePeriod(t *testing.T) {
	activePub, _ := mustKeyPair(t)
	retiredPub, retiredPriv := mustKeyPair(t)
	cfg := testAuthFile(t, activePub, retiredPub)

	validator, err := NewValidator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token := craftToken(t, retiredPriv, "dev-old", cfg.Auth.Issuer, cfg.Auth.Audience, KindClient, "alice", "", time.Hour)
	claims, err := validator.ParseToken(token)
	if err != nil {
		t.Fatalf("expected retired key to still validate during grace period: %v", err)
	}
	if claims.Subject != "alice" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestNewIssuerRejectsKeyNotInActiveSet(t *testing.T) {
	_, retiredPriv := mustKeyPair(t)
	activePub, _ := mustKeyPair(t)
	cfg := testAuthFile(t, activePub, nil)
	if _, err := NewIssuer(cfg, retiredPriv); err == nil {
		t.Fatalf("expected error when private key does not match any active key")
	}
}

// errCode extracts the stable error code from err, or "" if err is nil or
// not an *Error.
func errCode(err error) string {
	if err == nil {
		return ""
	}
	if aerr, ok := err.(*Error); ok {
		return aerr.Code
	}
	return ""
}

func errCode2(_ Claims, err error) string { return errCode(err) }

// craftToken builds a token independent of the Issuer type so tests can
// exercise malformed claims (wrong issuer/audience/kid) that the real
// Issuer would refuse to produce.
func craftToken(t *testing.T, priv ed25519.PrivateKey, kid, issuer, audience string, kind Kind, subject, serverName string, lifetime time.Duration) string {
	t.Helper()
	extra := map[string]any{ClaimKind: string(kind)}
	if serverName != "" {
		extra[ClaimServerName] = serverName
	}
	return craftTokenWithIssAud(t, priv, kid, issuer, audience, subject, lifetime, extra)
}

func craftTokenRaw(t *testing.T, priv ed25519.PrivateKey, kid, issuer, audience, subject string, lifetime time.Duration, extra map[string]any) string {
	t.Helper()
	return craftTokenWithIssAud(t, priv, kid, issuer, audience, subject, lifetime, extra)
}

func craftTokenWithIssAud(t *testing.T, priv ed25519.PrivateKey, kid, issuer, audience, subject string, lifetime time.Duration, extra map[string]any) string {
	t.Helper()
	now := time.Now()
	b := jwt.NewBuilder().
		Issuer(issuer).
		Audience([]string{audience}).
		Subject(subject).
		IssuedAt(now).
		Expiration(now.Add(lifetime))
	for k, v := range extra {
		b = b.Claim(k, v)
	}
	tok, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	key, err := jwk.Import(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatal(err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSA(), key))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}
