#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/metis-samples.XXXXXX")"
BIN="$WORK/metis"
trap 'rm -rf "$WORK"' EXIT

cd "$ROOT"
go build -o "$BIN" ./cmd/metis

mapfile_compat() {
  while IFS= read -r line; do
    SAMPLE_FILES+=("$line")
  done
}

SAMPLE_FILES=()
if builtin help mapfile >/dev/null 2>&1; then
  mapfile -t SAMPLE_FILES < <(find examples -type f -name '*.ossie.yaml' | sort)
else
  mapfile_compat < <(find examples -type f -name '*.ossie.yaml' | sort)
fi

if [[ ${#SAMPLE_FILES[@]} -eq 0 ]]; then
  echo "FAIL: no Ossie samples found under examples/" >&2
  exit 1
fi

for model in "${SAMPLE_FILES[@]}"; do
  echo "[sample] validate $model"
  "$BIN" model validate --model "$model" >/dev/null
done

MODEL="examples/sales.ossie.yaml"

run_case() {
  local name="$1" dialect="$2" request="$3"
  local out="$WORK/${name}.${dialect}.json"
  "$BIN" query compile --model "$MODEL" --dialect "$dialect" --request-json "$request" >"$out"
  test -s "$out"
  echo "PASS sample/$dialect/$name"
}

cat >"$WORK/metric.json" <<'JSON'
{"model":"sales","metrics":["total_revenue"]}
JSON
cat >"$WORK/grouped.json" <<'JSON'
{"model":"sales","metrics":["total_revenue"],"dimensions":["region"]}
JSON
cat >"$WORK/filter-limit.json" <<'JSON'
{"model":"sales","metrics":["total_revenue"],"dimensions":["region"],"filters":[{"field":"region","op":"=","value":"APAC"}],"limit":10}
JSON

for dialect in duckdb doris; do
  run_case metric "$dialect" "$WORK/metric.json"
  run_case grouped "$dialect" "$WORK/grouped.json"
  run_case filter-limit "$dialect" "$WORK/filter-limit.json"
done

python3 - "$WORK" <<'PYJSON'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
for dialect, join, limit in [
    ("duckdb", 'JOIN "sales"."public"."customer"', "FETCH FIRST 10 ROWS ONLY"),
    ("doris", 'JOIN `sales`.`public`.`customer`', "LIMIT 10"),
]:
    def load(name):
        return json.loads((root / f"{name}.{dialect}.json").read_text())
    assert "SUM(orders.amount)" in load("metric")["sql"]
    assert join in load("grouped")["sql"]
    filtered = load("filter-limit")
    assert filtered["dialect"] == dialect.upper()
    assert "?" in filtered["sql"] and limit in filtered["sql"]
    assert filtered["parameters"] == [{"value": "APAC"}]
PYJSON

# The deployable demo model is separately exercised end-to-end by tests/e2e/smoke.sh;
# validating every Ossie file here guarantees newly added samples cannot silently rot.
echo "Sample contracts: PASS (${#SAMPLE_FILES[@]} Ossie files validated)"
