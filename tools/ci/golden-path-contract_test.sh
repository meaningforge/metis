#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

quickstart_model="examples/demo/models/sales.ossie.yaml"
quickstart_manifest="examples/demo/metis.yaml"

grep -Fq "$quickstart_model" README.md
grep -Fq "$quickstart_manifest" README.md
grep -Fq "quickstart_model=\"$quickstart_model\"" tools/verify-golden-path.sh
grep -Fq "quickstart_manifest=\"$quickstart_manifest\"" tools/verify-golden-path.sh

check_prereqs="$(awk '/^check:/ { print; exit }' Makefile)"
if [[ "$check_prereqs" != *"golden-path"* ]]; then
  echo "make check must include golden-path so ordinary PR CI executes the quickstart contract" >&2
  exit 1
fi

echo "Golden-path CI contract: PASS"
