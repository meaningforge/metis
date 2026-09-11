#!/usr/bin/env bash
set -euo pipefail

ci=.github/workflows/ci.yml
matrix=.github/workflows/real-engine-matrix.yml

if ! grep -q 'make test-duckdb-backend' "$ci"; then
  echo "default CI must run the embedded DuckDB Backend gate" >&2
  exit 1
fi
if grep -Eq 'make test-engine-(clickhouse|doris)-container' "$ci"; then
  echo "default CI must not run ClickHouse or Doris real-engine conformance" >&2
  exit 1
fi
if grep -Eq 'run_(clickhouse|doris)_execution|real-engine-impact\.sh' "$ci"; then
  echo "default CI must not retain service-engine impact routing" >&2
  exit 1
fi

if ! grep -q 'workflow_dispatch:' "$matrix"; then
  echo "real-engine matrix must be explicitly dispatchable" >&2
  exit 1
fi
if grep -q 'schedule:' "$matrix"; then
  echo "real-engine matrix must not run service engines on an automatic schedule" >&2
  exit 1
fi
if ! grep -Eq '^[[:space:]]+default:[[:space:]]+duckdb$' "$matrix"; then
  echo "real-engine matrix must default to DuckDB" >&2
  exit 1
fi
if ! grep -q 'inputs.engines' "$matrix"; then
  echo "real-engine matrix must accept an explicit engine selector" >&2
  exit 1
fi
if ! grep -q 'tests/conformance/cmd/engines' "$matrix"; then
  echo "real-engine matrix must validate selectors through the executable registry" >&2
  exit 1
fi
if ! grep -q 'make test-duckdb-backend' "$matrix" || ! grep -q 'test-engine-${ENGINE}-container' "$matrix"; then
  echo "real-engine matrix must dispatch DuckDB's embedded gate and service-engine container gates" >&2
  exit 1
fi

echo "Real-engine CI policy contract: PASS"
