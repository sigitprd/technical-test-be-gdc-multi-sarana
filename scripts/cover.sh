#!/usr/bin/env bash
#
# Runs the unit tests with the race detector and measures coverage across all
# production packages (-coverpkg), so code exercised by another package's
# tests counts too. Prints the coverage of each package and the total, and
# fails when the total is below the minimum.
#
# Usage: scripts/cover.sh [minimum-percent]   (default 95)

set -euo pipefail

min=${1:-95}
profile=coverage.out
log=coverage.log
module=$(go list -m)
# servicetest only holds the in-memory fakes used by the tests.
pkgs=$(go list ./... | grep -v /servicetest | paste -sd, -)

if ! go test -race -count=1 -covermode=atomic -coverpkg="$pkgs" -coverprofile="$profile" ./... >"$log" 2>&1; then
  cat "$log"
  exit 1
fi
rm -f "$log"

echo "Coverage per package:"
# The profile lists each block once per test binary; a block counts as covered
# when any binary ran it.
awk -v module="$module/" 'NR > 1 {
  stmts[$1] = $2
  if ($3 > 0) hit[$1] = 1
}
END {
  for (block in stmts) {
    pkg = block
    sub(/:.*/, "", pkg)
    sub(/\/[^\/]*$/, "", pkg)
    sub(module, "", pkg)
    total[pkg] += stmts[block]
    if (block in hit) covered[pkg] += stmts[block]
  }
  for (pkg in total) printf "  %6.1f%%  %s\n", 100 * covered[pkg] / total[pkg], pkg
}' "$profile" | sort -k2

go tool cover -html="$profile" -o coverage.html
total=$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%/, "", $3); print $3 }')
echo "Total: ${total}% (minimum ${min}%), details in coverage.html"

if ! awk -v total="$total" -v min="$min" 'BEGIN { exit !(total + 0 >= min) }'; then
  echo "Coverage is below ${min}%" >&2
  exit 1
fi
