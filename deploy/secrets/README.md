# Compose Secrets (dev/test only)

`bootstrap-secret.txt` is a **TEST-ONLY** shared cluster secret consumed by
`DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE` (see `docker-entrypoint.sh`'s
`_FILE` convention). The signing key secret used by these Compose files is
`testdata/sample-config/dev-signing-key.pem`, mounted directly as
`DATORIUMDB_SIGNING_KEY_FILE` since that variable already expects a file
path.

Never reuse either file outside local development or CI. Production
deployments should mint their own bootstrap secret and Ed25519 signing key
and deliver them via a real secrets manager or Swarm/Kubernetes secrets.
