#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
WORK="${TMPDIR:-/tmp}/metis-ossie-conformance"
BIN="$WORK/s2s"
OSSIE_REF="88e0011148283302c9a04cd0287e00e0b9d87354"
OSSIE_RAW="https://raw.githubusercontent.com/apache/ossie/${OSSIE_REF}"
OSSIE_BASE="${OSSIE_RAW}/examples"

# A local Git object database is an optional offline source, never a different
# fixture authority: every byte is read from the same hard-coded upstream commit.
fetch_official() {
  local relative="$1" output="$2"
  if [[ -n "${OSSIE_GIT_DIR:-}" ]]; then
    git --git-dir "$OSSIE_GIT_DIR" cat-file blob "$OSSIE_REF:$relative" >"$output"
  else
    curl -fsSL --connect-timeout 15 --max-time 90 --retry 2 "$OSSIE_RAW/$relative" -o "$output"
  fi
}

rm -rf "$WORK"
mkdir -p "$WORK/official/converters" "$WORK/fixtures" "$WORK/requests" "$WORK/out"

echo "[conformance] build s2s"
go build -o "$BIN" "$ROOT/cmd/s2s"

echo "[conformance] fetch pinned Apache Ossie official examples @ $OSSIE_REF"
fetch_official "examples/tpcds_semantic_model.yaml" "$WORK/official/tpcds_semantic_model.yaml"
fetch_official "examples/flights.yaml" "$WORK/official/flights.yaml"
fetch_official "converters/omni/tests/fixtures/fixtureA_osi.yaml" "$WORK/official/converters/omni.yaml"
fetch_official "converters/databricks/tests/fixtures/fixtureA_ossie.yaml" "$WORK/official/converters/databricks-a.yaml"
fetch_official "converters/databricks/tests/fixtures/fixtureB_ossie.yaml" "$WORK/official/converters/databricks-b.yaml"
fetch_official "converters/databricks/tests/fixtures/tpcds_ossie.yaml" "$WORK/official/converters/databricks-tpcds.yaml"
fetch_official "converters/salesforce/src/test/resources/examples/osiToSalesforce.yaml" "$WORK/official/converters/salesforce.yaml"
fetch_official "converters/gooddata/tests/fixtures/osi_tpcds.yaml" "$WORK/official/converters/gooddata.yaml"
fetch_official "converters/gsf/tests/fixtures/sales.ossie.yaml" "$WORK/official/converters/gsf.yaml"

echo "[conformance] validate official Core and ontology examples"
"$BIN" validate-model --model "$WORK/official/tpcds_semantic_model.yaml" >/dev/null
FLIGHTS_VALIDATE="$($BIN validate-model --model "$WORK/official/flights.yaml")"
grep -q 'ontology_concepts=' <<<"$FLIGHTS_VALIDATE"
grep -q 'ontology_mappings=' <<<"$FLIGHTS_VALIDATE"
for fixture in "$WORK"/official/converters/*.yaml; do
  "$BIN" validate-model --model "$fixture" >/dev/null
done

cat >"$WORK/fixtures/enums.yaml" <<'YAML'
version: "0.2.0.dev0"
semantic_model:
  - name: enum_coverage
    datasets:
      - name: values_table
        source: test.values_table
        fields:
          - name: f_string
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: f_string}, {dialect: SNOWFLAKE, expression: f_string}, {dialect: MDX, expression: f_string}, {dialect: TABLEAU, expression: f_string}, {dialect: DATABRICKS, expression: f_string}, {dialect: MAQL, expression: f_string}, {dialect: BIGQUERY, expression: f_string}]}
            dimension: {}
          - {name: f_integer, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: f_integer}]}, dimension: {}}
          - {name: f_decimal, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: f_decimal}]}, dimension: {}}
          - {name: f_float, datatype: Float, expression: {dialects: [{dialect: ANSI_SQL, expression: f_float}]}, dimension: {}}
          - {name: f_boolean, datatype: Boolean, expression: {dialects: [{dialect: ANSI_SQL, expression: f_boolean}]}, dimension: {}}
          - {name: f_date, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: f_date}]}, dimension: {}}
          - {name: f_time, datatype: Time, expression: {dialects: [{dialect: ANSI_SQL, expression: f_time}]}, dimension: {}}
          - {name: f_datetime, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: f_datetime}]}, dimension: {}}
          - {name: f_datetimetz, datatype: DateTimeTz, expression: {dialects: [{dialect: ANSI_SQL, expression: f_datetimetz}]}, dimension: {}}
          - {name: f_opaque, datatype: Opaque, expression: {dialects: [{dialect: ANSI_SQL, expression: f_opaque}]}, dimension: {}}
    metrics:
      - name: row_count
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: COUNT(values_table.f_string)}]}
YAML
"$BIN" validate-model --model "$WORK/fixtures/enums.yaml" >/dev/null

cat >"$WORK/fixtures/composite.yaml" <<'YAML'
version: "0.2.0.dev0"
semantic_model:
  - name: composite_model
    datasets:
      - name: order_lines
        source: sales.order_lines
        primary_key: [order_id, line_number]
        unique_keys: [[order_id, line_number]]
        fields:
          - {name: product_id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: product_id}]}, dimension: {}}
          - {name: variant_id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: variant_id}]}, dimension: {}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
      - name: products
        source: sales.products
        primary_key: [id, variant_id]
        unique_keys: [[id, variant_id]]
        fields:
          - {name: id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: id}]}, dimension: {}}
          - {name: variant_id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: variant_id}]}, dimension: {}}
          - {name: category, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: category}]}, dimension: {}}
    relationships:
      - name: order_lines_to_products
        from: order_lines
        to: products
        from_columns: [product_id, variant_id]
        to_columns: [id, variant_id]
    metrics:
      - name: total_amount
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(order_lines.amount)}]}
YAML
"$BIN" validate-model --model "$WORK/fixtures/composite.yaml" >/dev/null
cat >"$WORK/requests/composite.json" <<'JSON'
{"model":"composite_model","metrics":["total_amount"],"dimensions":["category"],"limit":10}
JSON

cat >"$WORK/fixtures/time-role.yaml" <<'YAML'
version: "0.2.0.dev0"
semantic_model:
  - name: time_role_model
    datasets:
      - name: events
        source: analytics.events
        fields:
          - {name: occurred_on, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: occurred_on}]}, dimension: {}}
          - name: audit_created_at
            datatype: DateTime
            expression: {dialects: [{dialect: ANSI_SQL, expression: audit_created_at}]}
            dimension: {is_time: false}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: total_amount
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(events.amount)}]}
YAML
"$BIN" validate-model --model "$WORK/fixtures/time-role.yaml" >/dev/null
cat >"$WORK/requests/implicit-time.json" <<'JSON'
{"model":"time_role_model","metrics":["total_amount"],"dimensions":[{"name":"occurred_on","grain":"month"}]}
JSON
cat >"$WORK/requests/explicit-nontime.json" <<'JSON'
{"model":"time_role_model","metrics":["total_amount"],"dimensions":[{"name":"audit_created_at","grain":"month"}]}
JSON

cat >"$WORK/requests/metric-only.json" <<'JSON'
{"metrics":["total_sales"],"limit":100}
JSON
cat >"$WORK/requests/date-relationship.json" <<'JSON'
{"metrics":["total_sales"],"dimensions":["d_year"],"limit":100}
JSON
for grain in year quarter month week day hour; do
  printf '{"metrics":["total_sales"],"dimensions":[{"name":"d_date","grain":"%s"}],"limit":100}\n' "$grain" >"$WORK/requests/time-grain-${grain}.json"
done
cat >"$WORK/requests/item-relationship.json" <<'JSON'
{"metrics":["sales_by_brand"],"dimensions":["i_brand"],"limit":100}
JSON
cat >"$WORK/requests/cross-dataset-metric.json" <<'JSON'
{"metrics":["customer_lifetime_value"],"dimensions":["c_customer_id"],"limit":100}
JSON
cat >"$WORK/requests/store-metric.json" <<'JSON'
{"metrics":["store_productivity"],"dimensions":["s_store_name"],"limit":100}
JSON
cat >"$WORK/requests/filter.json" <<'JSON'
{"metrics":["total_sales"],"dimensions":["s_state"],"filters":[{"field":"s_state","op":"=","value":"CA"}],"limit":25}
JSON
cat >"$WORK/requests/computed-dimension.json" <<'JSON'
{"metrics":["customer_lifetime_value"],"dimensions":["customer_full_name"],"limit":20}
JSON
cat >"$WORK/requests/omni-roundtrip.json" <<'JSON'
{"model":"sales","metrics":["total_revenue"],"dimensions":["o_orderdate"],"limit":40}
JSON
cat >"$WORK/requests/multi-metric-dimension.json" <<'JSON'
{"metrics":["total_sales","total_profit"],"dimensions":["d_year","i_brand"],"limit":50}
JSON
cat >"$WORK/requests/filter-matrix.json" <<'JSON'
{"metrics":["total_sales"],"dimensions":["s_state"],"filters":[{"field":"s_state","op":"in","value":["CA","NY"]},{"field":"s_city","op":"not_in","value":["Unknown"]},{"field":"d_year","op":">=","value":2024},{"field":"d_year","op":"<=","value":2026},{"field":"d_year","op":"!=","value":2025},{"field":"d_date_sk","op":">","value":0},{"field":"d_date_sk","op":"<","value":99999999},{"field":"d_date_sk","op":"between","value":[1,99999999]},{"field":"c_first_name","op":"is_null"},{"field":"c_email_address","op":"is_not_null"}],"limit":30}
JSON

run_case() {
  local dialect="$1" name="$2" model="$3" request="$4"
  local first="$WORK/out/${name}.${dialect}.json"
  "$BIN" gen-sql --model "$model" --dialect "$dialect" --request-json "$request" >"$first"
  for i in 2 3 4 5; do
    local again="$WORK/out/${name}.${dialect}.${i}.json"
    "$BIN" gen-sql --model "$model" --dialect "$dialect" --request-json "$request" >"$again"
    cmp -s "$first" "$again" || { echo "FAIL determinism: $name invocation $i differs" >&2; diff -u "$first" "$again" >&2 || true; exit 1; }
  done
  python3 - "$first" "$WORK/out/${name}.${dialect}.sql" "$name" <<'PYJSON'
import json, pathlib, sys
query = json.loads(pathlib.Path(sys.argv[1]).read_text())
pathlib.Path(sys.argv[2]).write_text(query["sql"])
if sys.argv[3] == "filter":
    assert query["parameters"] == [{"value": "CA"}]
PYJSON
  echo "PASS $dialect/$name sha256=$(file_sha256 "$first")"
}

file_sha256() { local file="$1"; if command -v sha256sum >/dev/null 2>&1; then sha256sum "$file" | awk '{print $1}'; return; fi; if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$file" | awk '{print $1}'; return; fi; echo "no SHA-256 utility found" >&2; return 1; }

run_error_case() {
  local name="$1" expected="$2"; shift 2; local first="$WORK/out/${name}.err"
  if "$BIN" gen-sql "$@" >"$WORK/out/${name}.sql" 2>"$first"; then echo "FAIL expected error: $name" >&2; exit 1; fi
  grep -q "$expected" "$first"
  for i in 2 3 4 5; do local again="$WORK/out/${name}.${i}.err"; if "$BIN" gen-sql "$@" >"$WORK/out/${name}.${i}.sql" 2>"$again"; then echo "FAIL expected error: $name invocation $i" >&2; exit 1; fi; cmp -s "$first" "$again" || { echo "FAIL error determinism: $name invocation $i differs" >&2; diff -u "$first" "$again" >&2 || true; exit 1; }; done
  echo "PASS error/$name sha256=$(file_sha256 "$first")"
}

TPCDS="$WORK/official/tpcds_semantic_model.yaml"
run_suite() {
  local dialect="$1"
  run_case "$dialect" metric-only "$TPCDS" "$WORK/requests/metric-only.json"
  run_case "$dialect" date-relationship "$TPCDS" "$WORK/requests/date-relationship.json"
  for grain in year quarter month week day hour; do run_case "$dialect" "time-grain-${grain}" "$TPCDS" "$WORK/requests/time-grain-${grain}.json"; done
  run_case "$dialect" item-relationship "$TPCDS" "$WORK/requests/item-relationship.json"
  run_case "$dialect" cross-dataset-metric "$TPCDS" "$WORK/requests/cross-dataset-metric.json"
  run_case "$dialect" store-metric "$TPCDS" "$WORK/requests/store-metric.json"
  run_case "$dialect" filter "$TPCDS" "$WORK/requests/filter.json"
  run_case "$dialect" computed-dimension "$TPCDS" "$WORK/requests/computed-dimension.json"
  run_case "$dialect" multi-metric-dimension "$TPCDS" "$WORK/requests/multi-metric-dimension.json"
  run_case "$dialect" filter-matrix "$TPCDS" "$WORK/requests/filter-matrix.json"
  run_case "$dialect" composite-relationship "$WORK/fixtures/composite.yaml" "$WORK/requests/composite.json"
  run_case "$dialect" implicit-time-role "$WORK/fixtures/time-role.yaml" "$WORK/requests/implicit-time.json"
  run_case "$dialect" omni-roundtrip "$WORK/official/converters/omni.yaml" "$WORK/requests/omni-roundtrip.json"
}

run_suite doris
run_suite duckdb

if "$BIN" gen-sql --model "$WORK/fixtures/time-role.yaml" --dialect doris --request-json "$WORK/requests/explicit-nontime.json" >"$WORK/out/explicit-nontime.sql" 2>"$WORK/out/explicit-nontime.err"; then echo "FAIL explicit is_time=false accepted a time grain" >&2; exit 1; fi
grep -q 'time grain can only be applied to a time dimension' "$WORK/out/explicit-nontime.err"

run_error_case omni-count 'metric dataset cannot be determined' --model "$WORK/official/converters/omni.yaml" --dialect duckdb --semantic-model sales --metric order_count
run_error_case salesforce-invalid-expression 'EXPRESSION_PARSE_FAILED' --model "$WORK/official/converters/salesforce.yaml" --dialect duckdb --semantic-model Customer_Orders_Model --metric total_revenue
# The gsf fixture's own model references a field it does not define. The caller
# named no such field, so this is the model author's to fix, not theirs.
run_error_case gsf-unknown-model-reference 'UNRESOLVED_SEMANTIC_REFERENCE' --model "$WORK/official/converters/gsf.yaml" --dialect duckdb --semantic-model sales --metric revenue_per_customer

grep -q 'SUM(store_sales.ss_ext_sales_price)' "$WORK/out/metric-only.doris.sql"
grep -q 'JOIN `tpcds`.`public`.`date_dim` AS `date_dim`' "$WORK/out/date-relationship.doris.sql"
for grain in year quarter month week day hour; do grep -q "DATE_TRUNC(\"${grain}\", \`date_dim\`.\`d_date\`)" "$WORK/out/time-grain-${grain}.doris.sql"; done
grep -q 'JOIN `tpcds`.`public`.`item` AS `item`' "$WORK/out/item-relationship.doris.sql"
grep -q 'COUNT(DISTINCT customer.c_customer_sk)' "$WORK/out/cross-dataset-metric.doris.sql"
grep -q 'NULLIF(SUM(store.s_number_employees), 0)' "$WORK/out/store-metric.doris.sql"
grep -Fq "= ?" "$WORK/out/filter.doris.sql"
grep -Fq "c_first_name || ' ' || c_last_name" "$WORK/out/computed-dimension.doris.sql"
grep -q 'SUM(store_sales.ss_net_profit)' "$WORK/out/multi-metric-dimension.doris.sql"
grep -q ' IN (' "$WORK/out/filter-matrix.doris.sql"
grep -q ' NOT IN (' "$WORK/out/filter-matrix.doris.sql"
grep -q ' BETWEEN ' "$WORK/out/filter-matrix.doris.sql"
grep -q ' IS NOT NULL' "$WORK/out/filter-matrix.doris.sql"
grep -q ' IS NULL' "$WORK/out/filter-matrix.doris.sql"
grep -q 'JOIN `sales`.`products` AS `products`' "$WORK/out/composite-relationship.doris.sql"
grep -q 'DATE_TRUNC("month", `events`.`occurred_on`)' "$WORK/out/implicit-time-role.doris.sql"
grep -q 'FROM `samples`.`tpch`.`orders` AS `orders`' "$WORK/out/omni-roundtrip.doris.sql"

grep -q 'SUM(store_sales.ss_ext_sales_price)' "$WORK/out/metric-only.duckdb.sql"
grep -q 'FROM "tpcds"."public"."store_sales" AS "store_sales"' "$WORK/out/metric-only.duckdb.sql"
grep -q 'JOIN "tpcds"."public"."date_dim" AS "date_dim"' "$WORK/out/date-relationship.duckdb.sql"
for grain in YEAR QUARTER MONTH WEEK DAY HOUR; do lower_grain="$(printf '%s' "$grain" | tr '[:upper:]' '[:lower:]')"; grep -q "DATE_TRUNC('${grain}', \"date_dim\".\"d_date\")" "$WORK/out/time-grain-${lower_grain}.duckdb.sql"; done
grep -q 'JOIN "tpcds"."public"."item" AS "item"' "$WORK/out/item-relationship.duckdb.sql"
grep -q 'COUNT(DISTINCT customer.c_customer_sk)' "$WORK/out/cross-dataset-metric.duckdb.sql"
grep -q 'NULLIF(SUM(store.s_number_employees), 0)' "$WORK/out/store-metric.duckdb.sql"
grep -Fq "= ?" "$WORK/out/filter.duckdb.sql"
grep -q 'FETCH FIRST 25 ROWS ONLY' "$WORK/out/filter.duckdb.sql"
grep -Fq "c_first_name || ' ' || c_last_name" "$WORK/out/computed-dimension.duckdb.sql"
grep -q 'SUM(store_sales.ss_net_profit)' "$WORK/out/multi-metric-dimension.duckdb.sql"
grep -q ' IN (' "$WORK/out/filter-matrix.duckdb.sql"
grep -q ' NOT IN (' "$WORK/out/filter-matrix.duckdb.sql"
grep -q ' BETWEEN ' "$WORK/out/filter-matrix.duckdb.sql"
grep -q ' IS NOT NULL' "$WORK/out/filter-matrix.duckdb.sql"
grep -q ' IS NULL' "$WORK/out/filter-matrix.duckdb.sql"
grep -q 'JOIN "sales"."products" AS "products"' "$WORK/out/composite-relationship.duckdb.sql"
grep -q "DATE_TRUNC('MONTH', \"events\".\"occurred_on\")" "$WORK/out/implicit-time-role.duckdb.sql"
grep -q 'FROM "samples"."tpch"."orders" AS "orders"' "$WORK/out/omni-roundtrip.duckdb.sql"
grep -q 'FETCH FIRST 40 ROWS ONLY' "$WORK/out/omni-roundtrip.duckdb.sql"

echo "Apache Ossie upstream semantic conformance: PASS"
