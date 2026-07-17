// Package auth issues and validates the Ed25519-signed JWTs DatoriumDB uses
// for smart-client and server-to-server bearer authentication.
//
// Trust material (issuer, audience, and active/retired public signing keys)
// comes from the establishment config's __auth.json (see
// tech-docs/AUTHENTICATION.md). Only the establishment server and operator
// workstations hold the private signing key referenced by
// DATORIUMDB_SIGNING_KEY_FILE; every other server validates tokens locally
// using the public key set built by NewValidator.
package auth
