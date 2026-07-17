package auth

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const defaultTokenLifetime = 3600 * time.Second

// Issuer signs client and machine tokens with a private Ed25519 key loaded
// from DATORIUMDB_SIGNING_KEY_FILE. Only the establishment server and
// operator workstations should construct an Issuer; every other server only
// needs a Validator.
type Issuer struct {
	privateKey ed25519.PrivateKey
	kid        string

	Issuer          string
	Audience        string
	ClientLifetime  time.Duration
	MachineLifetime time.Duration

	// Now overrides the clock used for iat/exp; defaults to time.Now.
	Now func() time.Time
}

// NewIssuerFromFile loads an Ed25519 private key from keyPath (PKCS8 PEM,
// the DATORIUMDB_SIGNING_KEY_FILE format) and binds it to the matching
// active key entry in cfg's __auth.json, so the signing kid always reflects
// trusted public config rather than a caller-supplied value.
func NewIssuerFromFile(cfg config.AuthFile, keyPath string) (*Issuer, error) {
	priv, err := loadEd25519PrivateKeyPEM(keyPath)
	if err != nil {
		return nil, err
	}
	return NewIssuer(cfg, priv)
}

// NewIssuer binds priv to its matching active key entry in cfg's
// __auth.json and returns an Issuer ready to sign tokens.
func NewIssuer(cfg config.AuthFile, priv ed25519.PrivateKey) (*Issuer, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("auth: private key is not Ed25519")
	}
	kid, err := findActiveKidForPublicKey(cfg, pub)
	if err != nil {
		return nil, err
	}
	if cfg.Auth.Issuer == "" {
		return nil, fmt.Errorf("auth: __auth.json auth.issuer is required")
	}
	if cfg.Auth.Audience == "" {
		return nil, fmt.Errorf("auth: __auth.json auth.audience is required")
	}
	return &Issuer{
		privateKey:      priv,
		kid:             kid,
		Issuer:          cfg.Auth.Issuer,
		Audience:        cfg.Auth.Audience,
		ClientLifetime:  lifetimeOrDefault(cfg.Auth.TokenLifetimeSeconds.Client),
		MachineLifetime: lifetimeOrDefault(cfg.Auth.TokenLifetimeSeconds.Machine),
	}, nil
}

func lifetimeOrDefault(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultTokenLifetime
	}
	return time.Duration(seconds) * time.Second
}

// Kid returns the signing key's key ID, as recorded in __auth.json.
func (i *Issuer) Kid() string { return i.kid }

func (i *Issuer) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

// IssueClientToken signs a client token for subject. lifetime of 0 uses the
// issuer's configured client lifetime.
func (i *Issuer) IssueClientToken(subject string, lifetime time.Duration) (token string, actual time.Duration, err error) {
	if subject == "" {
		return "", 0, fmt.Errorf("auth: client token subject is required")
	}
	if lifetime <= 0 {
		lifetime = i.ClientLifetime
	}
	return i.issue(KindClient, subject, "", lifetime)
}

// IssueMachineToken signs a machine token embedding serverName as the
// datoriumdb.serverName claim. lifetime of 0 uses the issuer's configured
// machine lifetime.
func (i *Issuer) IssueMachineToken(serverName string, lifetime time.Duration) (token string, actual time.Duration, err error) {
	if serverName == "" {
		return "", 0, fmt.Errorf("auth: machine token serverName is required")
	}
	if lifetime <= 0 {
		lifetime = i.MachineLifetime
	}
	return i.issue(KindMachine, serverName, serverName, lifetime)
}

func (i *Issuer) issue(kind Kind, subject, serverName string, lifetime time.Duration) (string, time.Duration, error) {
	now := i.now()
	exp := now.Add(lifetime)
	b := jwt.NewBuilder().
		Issuer(i.Issuer).
		Audience([]string{i.Audience}).
		Subject(subject).
		IssuedAt(now).
		Expiration(exp).
		Claim(ClaimKind, string(kind))
	if serverName != "" {
		b = b.Claim(ClaimServerName, serverName)
	}
	tok, err := b.Build()
	if err != nil {
		return "", 0, fmt.Errorf("auth: build token: %w", err)
	}
	key, err := jwk.Import(i.privateKey)
	if err != nil {
		return "", 0, fmt.Errorf("auth: import signing key: %w", err)
	}
	if err := key.Set(jwk.KeyIDKey, i.kid); err != nil {
		return "", 0, fmt.Errorf("auth: set kid: %w", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSA(), key))
	if err != nil {
		return "", 0, fmt.Errorf("auth: sign token: %w", err)
	}
	return string(signed), lifetime, nil
}
