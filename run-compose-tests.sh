#!/usr/bin/env bash
# Run DatoriumDB's Docker Compose end-to-end tests (test/compose).
#
# Requires a working Docker daemon and the `docker compose` plugin. The Go
# suite builds images via the repo Dockerfile and brings up the topologies
# under deploy/*.yml (see DEVELOPERS.md and test/TRACEABILITY.md).
#
# Usage:
#   ./run-compose-tests.sh                  # full compose suite
#   ./run-compose-tests.sh -run TestComposeSingleNodeCRUD
#   ./run-compose-tests.sh -run 'TestComposeFiveShard|TestComposeDegraded'
#
# Extra arguments are forwarded to `go test`.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

die() {
  echo "run-compose-tests: $*" >&2
  exit 1
}

command -v go >/dev/null 2>&1 || die "go not found on PATH"
command -v docker >/dev/null 2>&1 || die "docker not found on PATH"

if ! docker info >/dev/null 2>&1; then
  die "docker daemon is not reachable (try: docker info)"
fi

if ! docker compose version >/dev/null 2>&1; then
  die "docker compose plugin is not available (try: docker compose version)"
fi

echo "run-compose-tests: docker $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo '?')"
echo "run-compose-tests: $(docker compose version)"
echo "run-compose-tests: go test -tags compose ./test/compose/..."

exec go test -tags compose ./test/compose/... -count=1 -v -timeout 20m "$@"
