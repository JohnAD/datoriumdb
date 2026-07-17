# Authentication

DatoriumDB should use bearer-token authentication for both smart-client requests and server-to-server requests.

The long-term goal is compatibility with standard token systems such as OAuth2/OIDC. However, the MVP must not require an external database-backed authentication server, because that would create an awkward dependency for a database project.

For the MVP, establishment config is the central authority source. The establishment server reads trusted auth configuration from `/db/.config`, serves the combined establishment document, and gives every DatoriumDB server the public information needed to validate tokens locally.

## Goals

- Allow smart clients to authenticate once and talk to all DatoriumDB servers they are authorized to use.
- Use the same general pattern for server-to-server communication.
- Avoid calling an auth server on every database request.
- Avoid requiring an external database-backed auth system for MVP bootstrapping.
- Keep the design compatible with external OIDC providers later.

## Token Pattern

Requests use bearer tokens:

```text
Authorization: Bearer {token}
```

For the MVP, tokens should be signed JWT-like tokens.

In Go, DatoriumDB uses [`lestrrat-go/jwx`](https://github.com/lestrrat-go/jwx) for JOSE work: JWT issuance and validation, JWK handling for `__auth.json`, and later JWE if encrypted JSON storage uses JOSE envelopes. Prefer a current major line such as `github.com/lestrrat-go/jwx/v3` or `v4`.

Every DatoriumDB server should validate tokens locally using trusted public key material from establishment config. A server should not need to call the establishment server or an auth server for every request.

## Authority Source

The establishment server is the central authority source for MVP authentication because it serves the trusted config from `/db/.config`.

This does not mean the establishment server must permanently be a full identity provider. It means the MVP trust root is file-backed and establishment-served.

Later versions may delegate token issuance and identity management to an external OIDC-compatible system.

## Config Storage

Auth-related public configuration is stored in `/db/.config/__auth.json`.

That filename matches the other database-wide config files under `/db/.config` (`__general.json`, `__servers.json`, `__shard-map.json`). The establishment server reads it and includes the public auth material in the combined establishment response.

`__auth.json` should support:

- issuer name
- token audience
- active public signing keys
- retired public signing keys kept temporarily for token grace periods
- token lifetime defaults
- machine bootstrap policy

Conceptually:

```json
{
  "auth": {
    "issuer": "https://db.mydomain.com",
    "audience": "datoriumdb",
    "tokenLifetimeSeconds": {
      "client": 3600,
      "machine": 3600
    },
    "keys": [
      {
        "kid": "2026-07-primary",
        "alg": "EdDSA",
        "status": "active",
        "publicKey": "..."
      }
    ]
  }
}
```

The exact key encoding and claim defaults can still be refined. The filename and file role are fixed: public trust material lives in `__auth.json`.

Private signing keys and bootstrap secrets should not be stored in `__auth.json` or other Git-tracked config files unless they are explicitly test-only values.

## Smart-Client Flow

1. The smart client obtains a client token from the configured DatoriumDB auth mechanism.
2. The smart client calls `GET /datoriumdb/v1/establish` with the bearer token.
3. The establishment server validates the token.
4. The establishment server returns an `ok: true` envelope containing the combined establishment document.
5. The smart client uses that document to route requests to the correct DatoriumDB servers.
6. The smart client sends the same bearer token to the DatoriumDB servers it contacts.
7. Each server validates the token locally.

## Server Startup Flow

Each DatoriumDB server starts with:

1. its own server name
2. the establishment server base URL

The server also needs an environment-provided bootstrap credential:

```text
DATORIUMDB_MACHINE_BOOTSTRAP_SECRET=...
```

That name is the MVP bootstrap credential. It is a shared cluster secret present in the environment of every server, including the establishment server. The establishment server validates it when issuing machine tokens. The bootstrap secret is never stored in `__auth.json` or other Git-tracked config. Per-server bootstrap secrets can be added later if needed.

### Machine Token Endpoint

Non-establishment servers obtain and renew machine tokens from the establishment server:

```text
POST /datoriumdb/v1/auth/machine-token
Content-Type: application/json
```

Request body:

```json
{
  "serverName": "serverB",
  "bootstrapSecret": "..."
}
```

Alternatively, a not-yet-expired machine token may be sent as `Authorization: Bearer {token}` instead of including `bootstrapSecret`, so servers can renew without reusing the bootstrap secret on every refresh.

On success:

```text
{
  ok: true,
  token: "...",
  expiresIn: 3600
}
```

Only the establishment server and operator workstations hold `DATORIUMDB_SIGNING_KEY_FILE`. Worker servers validate tokens with public keys from `__auth.json` and never hold the private signing key.

### Establishment Self-Start

The establishment server reads `/db/.config` locally. It does not call HTTP `/establish` against itself.

### Startup Flow

1. The server starts with its own server name and the establishment server base URL.
2. The establishment server loads local `/db/.config` and begins serving. Other servers read `DATORIUMDB_MACHINE_BOOTSTRAP_SECRET`.
3. A non-establishment server calls `POST /datoriumdb/v1/auth/machine-token` with its server name and bootstrap secret.
4. The server calls `GET /datoriumdb/v1/establish` with the machine bearer token.
5. The server receives an `ok: true` envelope containing the combined establishment document.
6. The server updates its local `/db/.config` cache from the establishment response and creates any missing collection directories.
7. The server learns its roles from `general.establishmentServer` and `shardMap`.
8. The server refreshes its machine token on a timer before expiry using the machine-token endpoint.
9. The server begins accepting only the requests allowed by its roles.

If the server cannot authenticate and does not have an acceptable local cached config, it should shut back down. Cached config is acceptable only when all required files are present; the server must still attempt an immediate background re-fetch and shut down if repeated refresh attempts fail.

## Server-To-Server Flow

Server-to-server calls should use the same bearer-token pattern as smart-client calls.

This includes:

- replication patches
- search patches
- cache updates
- historic schema requests
- config refreshes

Each receiving server validates the caller's token locally and verifies that the token represents an authorized machine identity.

## Token Claims

MVP tokens use these claims:

| Claim | Required | Meaning |
| --- | --- | --- |
| `iss` | yes | issuer from `__auth.json` |
| `aud` | yes | audience from `__auth.json` |
| `sub` | yes | subject identity string |
| `iat` | yes | issued-at time |
| `exp` | yes | expiration time |
| `datoriumdb.kind` | yes | `client` or `machine` |
| `datoriumdb.serverName` | machine only | server name matching `__servers.json` |

The JWT header should include `kid` matching an active or retired public key in `__auth.json`.

MVP authorization is authentication only:

- any valid client or machine token with correct `iss`, `aud`, and `exp` may call smart-client endpoints such as `/command` and `/establish`
- server-to-server `/sys` endpoints require `datoriumdb.kind` = `machine`
- the authenticated `datoriumdb.serverName` must match the `serverName` whose work is requested, fetched, applied, or deleted

Stable auth error codes include `unauthenticated`, `invalidToken`, `tokenExpired`, and `machineIdentityMismatch`.

Client tokens may eventually include collection-level or operation-level authorization, but partial schema visibility by authorization scope is not part of the MVP.

## Key Rotation

The config should allow more than one public signing key to be trusted at the same time.

This allows a new key to be introduced before the old key is retired.

`datoriumctl` manages `__auth.json`, including public key metadata, and increments `general.version` when auth config changes affect the combined establishment response. See [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md).

## Token Lifetimes

MVP defaults in `__auth.json` are:

- client tokens: `3600` seconds (1 hour)
- machine tokens: `3600` seconds (1 hour)

These are short-lived access tokens, not long-lived credentials. Machines should refresh their tokens on a timer before expiry after they have establishment config. The defaults can be changed later through `datoriumctl auth set` without changing the auth model.

## Token Issuance

For the MVP, `datoriumctl` can issue client and machine tokens. This is an operator bootstrap and demo path, not a full identity or user-management system.

Token signing uses a private key kept outside `/db/.config`, typically provided by:

```text
DATORIUMDB_SIGNING_KEY_FILE=/path/to/private-key.pem
```

Only public key material belongs in `__auth.json`. Later versions may let an external OIDC provider issue client tokens while `datoriumctl` remains available as an operator escape hatch.

## External Auth Later

Later versions may integrate with an external OIDC-compatible provider.

In that model:

- the external provider issues tokens
- DatoriumDB servers validate those tokens locally using OIDC/JWKS metadata
- establishment config tells servers which issuer, audience, and public keys are trusted

External providers should remain optional. DatoriumDB should be able to bootstrap without requiring another database-backed service.

## MVP Scope

The MVP should support:

- bearer-token authentication
- local token validation by every DatoriumDB server
- file-backed establishment auth metadata in `__auth.json`
- server bootstrap through `DATORIUMDB_MACHINE_BOOTSTRAP_SECRET`
- runtime machine token issuance and renewal through `POST /datoriumdb/v1/auth/machine-token`
- machine tokens for server-to-server calls
- client tokens for smart-client calls
- operator token issuance through `datoriumctl`
- default client and machine token lifetimes of `3600` seconds
- authentication-only authorization, plus machine-identity matching on `/sys` APIs

The MVP does not need:

- a full user-management system
- external OIDC integration
- partial schema visibility by authorization scope
- per-document authorization rules
- long-lived tokens
