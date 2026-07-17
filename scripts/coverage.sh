#!/usr/bin/env bash
# Coverage gate for DatoriumDB's core packages.
#
# Computes whole-module statement coverage for each core package (i.e.
# coverage attributed to a package's code no matter which test exercises
# it, via `-coverpkg`), using the default fast unit-test suite plus the
# in-process `contract` build tag (both are fast enough to run on every
# CI invocation; subprocess-based `integration`/`crash` suites are not
# included here because coverage instrumentation cannot follow across a
# subprocess boundary without a separate GOCOVERDIR merge step -- see
# test/TRACEABILITY.md and README.md "Coverage" section).
#
# The MVP target is >=80% for engine, fsstore, accesslang, config, and
# shard. Several packages are not there yet (their floors below reflect
# actual current coverage rounded down by a couple of points, not the
# 80% aim), so this script also documents the gap rather than silently
# accepting it. Any regression below a package's floor fails CI.
#
# Usage: scripts/coverage.sh [--update-floors]
#   --update-floors   Recompute and print floors from current coverage
#                      instead of gating (useful after adding tests).

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

declare -A FLOORS=(
  [engine]=78
  [fsstore]=70
  [accesslang]=80
  [config]=72
  [shard]=70
)
# Historical high-water marks, for humans reading this file; not enforced.
# engine ~81-83%, fsstore ~75%, accesslang ~86%, config ~76%, shard ~75%.

UPDATE_FLOORS=0
if [[ "${1:-}" == "--update-floors" ]]; then
  UPDATE_FLOORS=1
fi

REPORT="coverage-report.txt"
: > "$REPORT"

fail=0
for pkg in "${!FLOORS[@]}"; do
  profile="coverage-${pkg}.out"
  if ! go test -tags contract ./... -run . -coverpkg="./internal/${pkg}/..." -coverprofile="$profile" >/tmp/coverage-${pkg}.log 2>&1; then
    echo "FAIL ${pkg}: test run failed (see /tmp/coverage-${pkg}.log)"
    cat "/tmp/coverage-${pkg}.log"
    fail=1
    continue
  fi
  pct_raw=$(go tool cover -func="$profile" | tail -1 | awk '{print $3}' | tr -d '%')
  # go tool cover prints e.g. "81.5" or "81.5%"; guard against no
  # statements (empty output) by defaulting to 0.
  pct="${pct_raw:-0}"
  floor="${FLOORS[$pkg]}"
  line=$(printf "%-12s coverage=%6.1f%%  floor=%d%%" "$pkg" "$pct" "$floor")
  if awk -v p="$pct" -v f="$floor" 'BEGIN{exit !(p+0 < f+0)}'; then
    echo "FAIL $line" | tee -a "$REPORT"
    fail=1
  else
    echo "OK   $line" | tee -a "$REPORT"
  fi
done

echo
if [[ "$UPDATE_FLOORS" -eq 1 ]]; then
  echo "Current coverage (for updating FLOORS above):"
  cat "$REPORT"
  exit 0
fi

if [[ "$fail" -ne 0 ]]; then
  echo "Coverage gate FAILED. See $REPORT and per-package logs above." >&2
  exit 1
fi
echo "Coverage gate passed. See $REPORT for details."
