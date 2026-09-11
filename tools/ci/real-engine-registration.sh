#!/usr/bin/env bash
set -euo pipefail

engines=()
while IFS= read -r engine; do
  engines+=("$engine")
done < <(go run ./tests/conformance/cmd/engines -select all | tr -d '[]"' | tr ',' '\n' | sed '/^$/d')

if [[ ${#engines[@]} -eq 0 ]]; then
  echo "no registered real engines" >&2
  exit 1
fi

for engine in "${engines[@]}"; do
  engine="$(printf '%s' "$engine" | xargs)"
  dir="tests/engine/$engine"
  if [[ ! -d "$dir" ]]; then
    echo "registered real engine $engine is missing $dir" >&2
    exit 1
  fi
  if ! grep -Rq 'RunBackendResilienceContract' "$dir"; then
    echo "registered real engine $engine is missing the shared resilience contract" >&2
    exit 1
  fi
  if [[ "$engine" == "duckdb" ]]; then
    if ! grep -Eq '^test-duckdb-backend:' Makefile; then
      echo "registered real engine duckdb is missing Make target test-duckdb-backend" >&2
      exit 1
    fi
    if ! CGO_ENABLED=1 go list -tags duckdb "./$dir" >/dev/null 2>&1; then
      echo "registered real engine duckdb does not expose its tagged Go test package" >&2
      exit 1
    fi
    continue
  fi
  run="$dir/run.sh"
  target="test-engine-$engine-container"
  if [[ ! -f "$run" ]]; then
    echo "registered real engine $engine is missing $run" >&2
    exit 1
  fi
  if ! go list "./$dir" >/dev/null 2>&1; then
    echo "registered real engine $engine does not expose a Go test package at $dir" >&2
    exit 1
  fi
  if ! grep -Eq "^${target}:" Makefile; then
    echo "registered real engine $engine is missing Make target $target" >&2
    exit 1
  fi

done

echo "Real-engine registration contract: PASS"
