#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

cd "$repo_root"
go run ./cmd/s2sbench/bench/runner/readiness/cmd/project --output "$work_dir/bundle" >/dev/null

okf_module="github.com/okfcli/okf/cmd/okf@v0.4.0"
go run "$okf_module" validate "$work_dir/bundle" >/dev/null
go run "$okf_module" lint "$work_dir/bundle" >/dev/null
go run "$okf_module" graph "$work_dir/bundle" >/dev/null

echo "OKF v0.2 bundle validation: PASS (okfcli v0.4.0)"
