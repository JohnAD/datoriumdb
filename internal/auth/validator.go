package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Validator validates DatoriumDB bearer tokens locally, using the public
// key set, issuer, and audience trusted from an establishment config's
// __auth.json. Every DatoriumDB server builds and uses its own Validator;
// none of them call the establishment server per-request.
type Validator struct {
	issuer   string
	audience string
	keySet   jwk.Set

	// Now overrides the clock used to check exp/iat/nbf; defaults to time.Now.
	Now func() time.Time
}

// NewValidator builds a Validator from an __auth.json document.
func NewValidator(cfg config.AuthFile) (*Validator, error) {
	set, err := buildKeySet(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Auth.Issuer == "" {
		return nil, errors.New("auth: __auth.json auth.issuer is required")
	}
	if cfg.Auth.Audience == "" {
		return nil, errors.New("auth: __auth.json auth.audience is required")
	}
	return &Validator{issuer: cfg.Auth.Issuer, audience: cfg.Auth.Audience, keySet: set}, nil
}

func (v *Validator) clock() jwt.Clock {
	now := v.Now
	if now == nil {
		now = time.Now
	}
	return jwt.ClockFunc(now)
}

// ParseBearer extracts and validates a bearer token from the value of an
// Authorization header. Returns *Error with code "unauthenticated" if the
// header is missing or malformed.
func (v *Validator) ParseBearer(headerValue string) (Claims, error) {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return Claims{}, errUnauthenticated("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(headerValue, prefix) {
		return Claims{}, errUnauthenticated("Authorization header must use the Bearer scheme")
	}
	raw := strings.TrimSpace(headerValue[len(prefix):])
	if raw == "" {
		return Claims{}, errUnauthenticated("missing bearer token")
	}
	return v.ParseToken(raw)
}

// ParseToken validates raw as a compact JWS and returns its DatoriumDB
// claims. Returns *Error with a stable code on any validation failure.
func (v *Validator) ParseToken(raw string) (Claims, error) {
	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(v.keySet),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithClock(v.clock()),
	)
	if err != nil {
		if errors.Is(err, jwt.TokenExpiredError()) {
			return Claims{}, errTokenExpired("token is expired")
		}
		return Claims{}, errInvalidToken(err.Error())
	}

	claims := Claims{}
	if sub, ok := tok.Subject(); ok {
		claims.Subject = sub
	}
	if iss, ok := tok.Issuer(); ok {
		claims.Issuer = iss
	}
	if aud, ok := tok.Audience(); ok {
		claims.Audience = aud
	}
	if iat, ok := tok.IssuedAt(); ok {
		claims.IssuedAt = iat
	}
	if exp, ok := tok.Expiration(); ok {
		claims.ExpiresAt = exp
	}

	var kindRaw string
	if err := tok.Get(ClaimKind, &kindRaw); err != nil {
		return Claims{}, errInvalidToken("token is missing the " + ClaimKind + " claim")
	}
	switch Kind(kindRaw) {
	case KindClient, KindMachine:
		claims.Kind = Kind(kindRaw)
	default:
		return Claims{}, errInvalidToken("token has an unknown " + ClaimKind + " claim: " + kindRaw)
	}

	if claims.Kind == KindMachine {
		var serverName string
		if err := tok.Get(ClaimServerName, &serverName); err != nil || serverName == "" {
			return Claims{}, errInvalidToken("machine token is missing the " + ClaimServerName + " claim")
		}
		claims.ServerName = serverName
	}

	return claims, nil
}

// RequireMachine returns a *Error with code "machineIdentityMismatch" if
// claims does not represent a machine token, or (when serverName is
// non-empty) if the authenticated machine identity does not match
// serverName. This implements the /sys authorization rule from
// tech-docs/AUTHENTICATION.md: "the authenticated datoriumdb.serverName must
// match the serverName whose work is requested, fetched, applied, or
// deleted."
func RequireMachine(claims Claims, serverName string) error {
	if claims.Kind != KindMachine {
		return errMachineIdentityMismatch("this endpoint requires a machine token")
	}
	if serverName != "" && claims.ServerName != serverName {
		return errMachineIdentityMismatch("authenticated server name does not match the requested server name")
	}
	return nil
}
