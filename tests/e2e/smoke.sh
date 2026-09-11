#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${TMPDIR:-/tmp}/metis-smoke"
LOG="${TMPDIR:-/tmp}/metis-smoke.log"
ADDR="127.0.0.1:18080"
BASE="http://${ADDR}"
API_KEY="metis-smoke-key"
PID=""

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    kill -TERM "${PID}" || true
    wait "${PID}" || true
  fi
  rm -f "${BIN}"
}
trap cleanup EXIT

cd "${ROOT}"
go build -o "${BIN}" ./cmd/metis

for command in git deploy; do
  if "${BIN}" "$command" >"${LOG}" 2>&1; then
    echo "CLI unexpectedly exposes unsupported command $command" >&2
    exit 1
  fi
done

version_output="$(${BIN} version)"
grep -q '"version":"dev"' <<<"${version_output}"

METIS_API_KEY="${API_KEY}" "${BIN}" serve \
  --config "${ROOT}/examples/demo/metis.yaml" \
  --addr "${ADDR}" \
  --metrics \
  --shutdown-timeout 5s >"${LOG}" 2>&1 &
PID=$!

ready=0
for _ in $(seq 1 60); do
  if curl -fsS "${BASE}/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "${PID}" 2>/dev/null; then
    echo "Metis exited before becoming ready" >&2
    cat "${LOG}" >&2 || true
    wait "${PID}" || true
    PID=""
    exit 1
  fi
  sleep 0.25
done
if [[ "${ready}" != "1" ]]; then
  echo "Metis did not become ready at ${BASE}/readyz" >&2
  cat "${LOG}" >&2 || true
  exit 1
fi

health_response="$(curl -fsS "${BASE}/healthz")"
ready_response="$(curl -fsS "${BASE}/readyz")"
grep -q '"status":"ok"' <<<"${health_response}"
grep -q '"project":"demo"' <<<"${ready_response}"
if grep -Eq '"(digest|generation)"' <<<"${ready_response}"; then
  echo "Readiness must not expose a deployment-wide digest or generation" >&2
  exit 1
fi
metrics_response="$(curl -fsS "${BASE}/metrics")"
grep -q '^go_goroutines ' <<<"${metrics_response}"
grep -q '^process_cpu_seconds_total ' <<<"${metrics_response}"

unauthorized_status="$(curl -sS -o /dev/null -w '%{http_code}' "${BASE}/v1/projects/demo/semantics/search?q=revenue")"
[[ "${unauthorized_status}" == "401" ]]

search_response="$(curl -fsS \
  -H "Authorization: Bearer ${API_KEY}" \
  "${BASE}/v1/projects/demo/semantics/search?q=revenue")"
grep -q 'total_revenue' <<<"${search_response}"

missing_metric_url="${BASE}/v1/projects/demo/models/sales/metrics/does_not_exist"
missing_metric_status="$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${API_KEY}" "${missing_metric_url}")"
[[ "${missing_metric_status}" == "404" ]]
missing_metric_response="$(curl -sS -H "Authorization: Bearer ${API_KEY}" "${missing_metric_url}")"
grep -q '"code":"METRIC_NOT_FOUND"' <<<"${missing_metric_response}"
grep -q '"caller_action":"CHANGE_REQUEST"' <<<"${missing_metric_response}"
if grep -q '"class"' <<<"${missing_metric_response}"; then
  echo "REST error payload still carries the removed class field" >&2
  exit 1
fi

malformed_compile="$(curl -sS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  "${BASE}/v1/compile-sql" \
  -d '{"query": "not an object"}')"
grep -q '"code":"INVALID_QUERY"' <<<"${malformed_compile}"
grep -q '"caller_action":"CHANGE_REQUEST"' <<<"${malformed_compile}"

context_response="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  "${BASE}/v1/projects/demo/models/sales/semantic-context" \
  -d '{"metrics":["total_revenue"],"dimensions":["region"]}')"
grep -q '"project":"demo"' <<<"${context_response}"
grep -q '"model":"sales"' <<<"${context_response}"
grep -q '"name":"total_revenue"' <<<"${context_response}"
grep -q '"qualified_name":"orders.region"' <<<"${context_response}"

compile_request='{"dialect":"DUCKDB","query":{"project":"demo","model":"sales","metrics":[{"name":"total_revenue"}],"dimensions":[{"name":"region"}]}}'
compile_response="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  "${BASE}/v1/compile-sql" \
  -d "${compile_request}")"
grep -q '"sql_statement":{' <<<"${compile_response}"
grep -q '"dialect":"DUCKDB"' <<<"${compile_response}"
grep -q 'SUM(orders.amount)' <<<"${compile_response}"
grep -q '"output_schema":{"columns":' <<<"${compile_response}"
grep -q '"name":"region","kind":"dimension","datatype":"String"' <<<"${compile_response}"
grep -q '"name":"total_revenue","kind":"metric","datatype":"Decimal"' <<<"${compile_response}"

explain_response="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  "${BASE}/v1/explain" \
  -d "${compile_request}")"
grep -q '"project":"demo"' <<<"${explain_response}"
grep -q '"model":"sales"' <<<"${explain_response}"
grep -q '"kind":"source"' <<<"${explain_response}"
grep -q '"kind":"aggregation"' <<<"${explain_response}"
grep -q '"kind":"grouping"' <<<"${explain_response}"
grep -q '"output_schema":{"columns":' <<<"${explain_response}"
if grep -q 'sql_statement' <<<"${explain_response}"; then
  echo "Explain response must not expose a physical query" >&2
  exit 1
fi
if grep -q 'duckdb-local\|"execution"\|"binding"' <<<"${compile_response}${explain_response}"; then
  echo "Compile and Explain responses must not expose transitional routing state" >&2
  exit 1
fi

mcp_initialize="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"metis-smoke","version":"0"}}}')"
grep -q '"name":"metis"' <<<"${mcp_initialize}"

mcp_tools="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')"
for primary_tool in list_projects list_models get_model list_metrics get_metric get_dimensions get_dimension get_relationships compile_sql query_metrics attribute_metric compare_metrics; do
  grep -q "${primary_tool}" <<<"${mcp_tools}"
done
for deleted_tool in search_semantics get_metric_dimensions get_semantic_context validate_query explain_query compile; do
  if grep -q "\"name\":\"${deleted_tool}\"" <<<"${mcp_tools}"; then
    echo "Deleted MCP tool ${deleted_tool} is still registered" >&2
    exit 1
  fi
done

mcp_projects="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}')"
grep -q 'demo' <<<"${mcp_projects}"

mcp_models="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"list_models","arguments":{}}}')"
grep -q '\"ref\":\"model:sales\"' <<<"${mcp_models}"

mcp_metrics="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_metrics","arguments":{"search":["revenue"]}}}')"
grep -q '\"ref\":\"metric:sales.total_revenue\"' <<<"${mcp_metrics}"
grep -q '\"model\":\"sales\"' <<<"${mcp_metrics}"

mcp_metric="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":41,"method":"tools/call","params":{"name":"get_metric","arguments":{"ref":"metric:sales.total_revenue"}}}')"
grep -q '\"ref\":\"metric:sales.total_revenue\"' <<<"${mcp_metric}"

mcp_dimensions="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_dimensions","arguments":{"metrics":["metric:sales.total_revenue"],"search":["region"]}}}')"
grep -q '\"ref\":\"dimension:sales.orders.region\"' <<<"${mcp_dimensions}"

mcp_dimension="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":51,"method":"tools/call","params":{"name":"get_dimension","arguments":{"ref":"dimension:sales.orders.region"}}}')"
grep -q '\"data_type\":\"String\"' <<<"${mcp_dimension}"

mcp_relationships="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":52,"method":"tools/call","params":{"name":"get_relationships","arguments":{"model":"model:sales","datasets":["orders"]}}}')"
grep -q '\"relationships\"' <<<"${mcp_relationships}"

mcp_compile="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"compile_sql","arguments":{"dialect":"DUCKDB","output_metrics":["metric:sales.total_revenue"],"group_by":[{"name":"dimension:sales.orders.region","type":"dimension"}]}}}')"
grep -q '"sql_statement":{' <<<"${mcp_compile}"
grep -q '"output_schema"' <<<"${mcp_compile}"
grep -q '"name":"orders.region"' <<<"${mcp_compile}"
grep -q '"kind":"dimension"' <<<"${mcp_compile}"
grep -q '"name":"total_revenue"' <<<"${mcp_compile}"
grep -q '"kind":"metric"' <<<"${mcp_compile}"

mcp_invalid_dimensions="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
	-H 'X-Metis-Project-Id: demo' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_dimensions","arguments":{"metrics":[]}}}')"
grep -q '\\"code\\":\\"INVALID_QUERY\\"' <<<"${mcp_invalid_dimensions}"
grep -q '\\"caller_action\\":\\"CHANGE_REQUEST\\"' <<<"${mcp_invalid_dimensions}"

runtime_metrics="$(curl -fsS "${BASE}/metrics")"
grep -q 'metis_compile_requests_total{dialect="DUCKDB",result="success"} 2' <<<"${runtime_metrics}"
grep -q 'metis_http_requests_total{method="POST",route="/v1/compile-sql",status_class="2xx"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="list_projects",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="list_models",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="list_metrics",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="get_metric",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="get_dimensions",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="get_dimensions",result="error"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="get_dimension",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="get_relationships",result="success"} 1' <<<"${runtime_metrics}"
grep -q 'metis_mcp_requests_total{method="compile_sql",result="success"} 1' <<<"${runtime_metrics}"
for phase in analysis planning optimization rendering; do
  grep -q "metis_compile_phase_duration_seconds_count{dialect=\"DUCKDB\",phase=\"${phase}\"} 2" <<<"${runtime_metrics}"
done
grep -Eq 'metis_optimizer_runs_total\{outcome="(noop|rewritten)"\} 2' <<<"${runtime_metrics}"

kill -TERM "${PID}"
wait "${PID}"
PID=""
grep -q '"msg":"shutdown signal received"' "${LOG}"
grep -q '"msg":"metis server stopped"' "${LOG}"

METIS_API_KEY="${API_KEY}" "${BIN}" serve \
  --config "${ROOT}/examples/multi-project/metis.yaml" \
  --addr "${ADDR}" \
  --shutdown-timeout 5s >"${LOG}" 2>&1 &
PID=$!

ready=0
for _ in $(seq 1 60); do
  if curl -fsS "${BASE}/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "${PID}" 2>/dev/null; then
    echo "Multi-project Metis exited before becoming ready" >&2
    cat "${LOG}" >&2 || true
    wait "${PID}" || true
    PID=""
    exit 1
  fi
  sleep 0.25
done
if [[ "${ready}" != "1" ]]; then
  echo "Multi-project Metis did not become ready" >&2
  cat "${LOG}" >&2 || true
  exit 1
fi

multi_ready="$(curl -fsS "${BASE}/readyz")"
grep -q '"projects":\["finance","growth"\]' <<<"${multi_ready}"
if grep -Eq '"(digest|generation)"' <<<"${multi_ready}"; then
  echo "Multi-project readiness must not expose aggregate semantic version state" >&2
  exit 1
fi
if grep -q '"project":' <<<"${multi_ready}"; then
  echo "Multi-project readiness must not imply a default project" >&2
  exit 1
fi

multi_projects="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}')"
grep -q 'finance' <<<"${multi_projects}"
grep -q 'growth' <<<"${multi_projects}"

multi_compile="$(curl -fsS -X POST \
  -H "Authorization: Bearer ${API_KEY}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "${BASE}/mcp" \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"compile_sql","arguments":{"project_id":"growth","dialect":"DUCKDB","output_metrics":["metric:sales.total_revenue"]}}}')"
grep -q '"dialect":"DUCKDB"' <<<"${multi_compile}"
grep -q 'SUM(orders.amount)' <<<"${multi_compile}"

kill -TERM "${PID}"
wait "${PID}"
PID=""
grep -q '"msg":"metis server stopped"' "${LOG}"

echo "Metis E2E smoke test passed"
