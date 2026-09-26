#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

quickstart_model="examples/demo/models/sales.ossie.yaml"
quickstart_manifest="examples/demo/metis.yaml"

go build -trimpath -o "$tmp/metis" ./cmd/metis

# Keep the release-facing README contract executable. These commands mirror the
# first-run path documented for a new user and must remain valid together.
"$tmp/metis" validate --project finance "$quickstart_model"
"$tmp/metis" inspect --project finance --file "$quickstart_model" --model sales >"$tmp/inspect.json"
"$tmp/metis" model validate --model "$quickstart_model" >"$tmp/model-validation.txt"
"$tmp/metis" project validate --project demo --config examples/demo/project.yaml >"$tmp/project-validation.json"
"$tmp/metis" version >"$tmp/version.json"
"$tmp/metis" query compile \
  --model "$quickstart_model" \
  --dialect DUCKDB \
  --metric total_revenue \
  --dimension region \
  --filter "region = 'APAC'" \
  --limit 100 >"$tmp/query.json"

grep -q 'total_revenue' "$tmp/inspect.json"
grep -q '"metric_names"' "$tmp/inspect.json"
grep -q 'ontology_concepts=' "$tmp/model-validation.txt"
grep -q '"valid": true' "$tmp/project-validation.json"
grep -q '"publishable": true' "$tmp/project-validation.json"
grep -q '"version"' "$tmp/version.json"
python3 - "$tmp/query.json" <<'PYJSON'
import json, sys
with open(sys.argv[1]) as source:
    query = json.load(source)
assert query["dialect"] == "DUCKDB"
assert "SELECT" in query["sql"] and "?" in query["sql"]
assert query["parameters"] == [{"value": "APAC"}]
PYJSON
grep -Fq "$quickstart_model" README.md
grep -Fq "$quickstart_manifest" README.md
grep -q 'default_project: demo' "$quickstart_manifest"
grep -q 'semantic_sources:' examples/demo/project.yaml
grep -q 'total_revenue' README.md
grep -q 'metis query compile' README.md

echo "Golden user path: PASS"
