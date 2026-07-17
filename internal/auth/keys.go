package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// buildKeySet turns the public keys listed in __auth.json (active and
// retired) into a jwk.Set usable for JWT verification. Retired keys stay in
// the trust set so tokens signed just before rotation keep validating during
// their grace period; only active keys are candidates for new issuance.
func buildKeySet(a config.AuthFile) (jwk.Set, error) {
	if len(a.Auth.Keys) == 0 {
		return nil, fmt.Errorf("auth: no signing keys configured in __auth.json")
	}
	set := jwk.NewSet()
	haveActive := false
	for _, k := range a.Auth.Keys {
		key, err := decodePublicKey(k)
		if err != nil {
			return nil, fmt.Errorf("auth: key %q: %w", k.Kid, err)
		}
		if err := set.AddKey(key); err != nil {
			return nil, fmt.Errorf("auth: key %q: %w", k.Kid, err)
		}
		if k.Status == "active" {
			haveActive = true
		}
	}
	if !haveActive {
		return nil, fmt.Errorf("auth: no active signing key configured in __auth.json")
	}
	return set, nil
}

func decodePublicKey(k config.AuthKey) (jwk.Key, error) {
	if k.Kid == "" {
		return nil, fmt.Errorf("kid is required")
	}
	if k.Alg != "EdDSA" {
		return nil, fmt.Errorf("unsupported alg %q (only EdDSA is supported)", k.Alg)
	}
	raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid publicKey base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key length %d", len(raw))
	}
	key, err := jwk.Import(ed25519.PublicKey(raw))
	if err != nil {
		return nil, err
	}
	if err := key.Set(jwk.KeyIDKey, k.Kid); err != nil {
		return nil, err
	}
	if err := key.Set(jwk.AlgorithmKey, jwa.EdDSA()); err != nil {
		return nil, err
	}
	return key, nil
}

// EncodePublicKey base64-encodes a raw Ed25519 public key for storage in
// __auth.json's "publicKey" field.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// loadEd25519PrivateKeyPEM reads a PKCS8 PEM-encoded Ed25519 private key,
// the format written by DATORIUMDB_SIGNING_KEY_FILE.
func loadEd25519PrivateKeyPEM(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key file: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("signing key file %s is not PEM encoded", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 private key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("signing key file %s does not contain an Ed25519 private key", path)
	}
	return priv, nil
}

// findActiveKidForPublicKey returns the kid of the active __auth.json key
// whose public key matches pub, binding the loaded private key to its
// trusted public counterpart rather than trusting a caller-supplied kid.
func findActiveKidForPublicKey(a config.AuthFile, pub ed25519.PublicKey) (string, error) {
	for _, k := range a.Auth.Keys {
		if k.Status != "active" || k.Alg != "EdDSA" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.PublicKey(raw).Equal(pub) {
			return k.Kid, nil
		}
	}
	return "", fmt.Errorf("signing key does not match any active key in __auth.json")
}
