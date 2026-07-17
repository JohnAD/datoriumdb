#!/bin/sh
# Entrypoint for the DatoriumDB container image.
#
# Supports the common "docker secret" _FILE convention for values that
# Compose/Swarm secrets deliver as files under /run/secrets rather than as
# plain environment variables: if DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE
# is set, its contents become DATORIUMDB_MACHINE_BOOTSTRAP_SECRET.
# DATORIUMDB_SIGNING_KEY_FILE already names a file path directly (see
# tech-docs/AUTHENTICATION.md), so it needs no such translation -- just
# point it at the mounted secret path.
set -eu

if [ -n "${DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE:-}" ]; then
    if [ ! -f "$DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE" ]; then
        echo "docker-entrypoint: DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE=$DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE does not exist" >&2
        exit 1
    fi
    DATORIUMDB_MACHINE_BOOTSTRAP_SECRET="$(cat "$DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE")"
    export DATORIUMDB_MACHINE_BOOTSTRAP_SECRET
fi

exec /usr/local/bin/datoriumdb "$@"
