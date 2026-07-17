package auth

import "time"

// Kind identifies whether a token represents a smart client or a machine
// (server-to-server) identity, per tech-docs/AUTHENTICATION.md.
type Kind string

const (
	KindClient  Kind = "client"
	KindMachine Kind = "machine"
)

// Private claim names used by DatoriumDB tokens, beyond the registered JWT
// claims (iss, aud, sub, iat, exp).
const (
	ClaimKind       = "datoriumdb.kind"
	ClaimServerName = "datoriumdb.serverName"
)

// Claims is the parsed, fully validated representation of a DatoriumDB
// bearer token.
type Claims struct {
	Subject    string
	Kind       Kind
	ServerName string
	Issuer     string
	Audience   []string
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

// Error is a stable auth error code paired with a human-readable message.
// Stable codes per tech-docs/AUTHENTICATION.md are: unauthenticated,
// invalidToken, tokenExpired, and machineIdentityMismatch. This package also
// uses a few closely related codes for the machine-token bootstrap flow.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errUnauthenticated(msg string) *Error {
	return &Error{Code: "unauthenticated", Message: msg}
}

func errInvalidToken(msg string) *Error {
	return &Error{Code: "invalidToken", Message: msg}
}

func errTokenExpired(msg string) *Error {
	return &Error{Code: "tokenExpired", Message: msg}
}

func errMachineIdentityMismatch(msg string) *Error {
	return &Error{Code: "machineIdentityMismatch", Message: msg}
}
