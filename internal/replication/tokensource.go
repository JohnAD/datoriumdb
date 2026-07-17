package replication

import (
	"context"

	"github.com/JohnAD/datoriumdb/internal/auth"
)

// IssuerTokenSource mints machine tokens for ServerName directly from an
// auth.Issuer. This lets a server that holds a signing key (typically the
// establishment server, when it also serves a shard role) authenticate its
// own server-to-server replication calls without a separate bootstrap
// round trip.
type IssuerTokenSource struct {
	Issuer     *auth.Issuer
	ServerName string
}

// Token implements TokenSource.
func (s IssuerTokenSource) Token(_ context.Context) (string, error) {
	token, _, err := s.Issuer.IssueMachineToken(s.ServerName, 0)
	return token, err
}

// StaticTokenSource returns a fixed token, useful for tests.
type StaticTokenSource string

// Token implements TokenSource.
func (s StaticTokenSource) Token(_ context.Context) (string, error) {
	return string(s), nil
}
