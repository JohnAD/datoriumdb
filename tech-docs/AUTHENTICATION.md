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

Every DatoriumDB server should validate tokens locally using trusted public key material from establishment config. A server should not need to call the establishment server or an auth server for every request.

## Authority Source

The establishment server is the central authority source for MVP authentication because it serves the trusted config from `/db/.config`.

This does not mean the establishment server must permanently be a full identity provider. It means the MVP trust root is file-backed and establishment-served.

Later versions may delegate token issuance and identity management to an external OIDC-compatible system.

## Config Storage

Auth-related public configuration should be stored in `/db/.config`.

The exact file layout is still to be finalized, but the config should support:

- issuer name
- token audience
- active public signing keys
- retired public signing keys kept temporarily for token grace periods
- token lifetime defaults
- machine bootstrap policy

Private signing keys and bootstrap secrets should not be stored in Git-tracked config files unless they are explicitly test-only values.

## Smart-Client Flow

1. The smart client obtains a client token from the configured DatoriumDB auth mechanism.
2. The smart client calls `GET /datoriumdb/v1/establish` with the bearer token.
3. The establishment server validates the token.
4. The establishment server returns the combined establishment document.
5. The smart client uses that document to route requests to the correct DatoriumDB servers.
6. The smart client sends the same bearer token to the DatoriumDB servers it contacts.
7. Each server validates the token locally.

## Server Startup Flow

Each DatoriumDB server starts with:

1. its own server name
2. the establishment server base URL

The server also needs an environment-provided bootstrap credential.

For example:

```text
DATORIUMDB_MACHINE_BOOTSTRAP_SECRET=...
```

The exact environment variable name is not final, but the purpose is important: the server needs some out-of-band secret so it can authenticate itself before it has fetched establishment config.

Startup flow:

1. The server starts with its own server name and the establishment server base URL.
2. The server reads its bootstrap credential from the environment.
3. The server uses the bootstrap credential to obtain or prove a machine identity.
4. The server calls `GET /datoriumdb/v1/establish`.
5. The server receives the combined establishment document.
6. The server updates its local `/db/.config` cache from the establishment response.
7. The server learns its roles from `general.establishmentServer` and `shardMap`.
8. The server begins accepting only the requests allowed by its roles.

If the server cannot authenticate and does not have an acceptable local cached config, it should shut back down.

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

The exact claim names are not final, but tokens should carry enough information to validate:

- issuer
- audience
- subject
- expiration
- token kind, such as client or machine
- database identity
- allowed operations or role scope

Machine tokens should identify the server name they represent.

Client tokens may eventually include collection-level or operation-level authorization, but partial schema visibility by authorization scope is not part of the MVP.

## Key Rotation

The config should allow more than one public signing key to be trusted at the same time.

This allows a new key to be introduced before the old key is retired.

The command-line config tool should manage key metadata and increment `general.version` when auth config changes affect the combined establishment response.

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
- file-backed establishment auth metadata
- server bootstrap through an environment-provided secret
- machine tokens for server-to-server calls
- client tokens for smart-client calls

The MVP does not need:

- a full user-management system
- external OIDC integration
- partial schema visibility by authorization scope
- per-document authorization rules
- long-lived tokens

## Open Questions

- What is the exact file name for auth metadata under `/db/.config`?
- What is the exact environment variable name for the machine bootstrap credential?
- Should the MVP command-line tool issue client tokens directly?
- What JWT library should be used in Go?
- What token lifetimes should be used for client and machine tokens?
