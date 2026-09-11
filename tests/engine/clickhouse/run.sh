#!/usr/bin/env bash
set -euo pipefail

CLICKHOUSE_VERSION="25.8"
source "$(dirname "${BASH_SOURCE[0]}")/../container/runtime.sh"
RUN_ID="$(metis_container_run_id)"
CONTAINER="metis-clickhouse-conformance-${RUN_ID}"

cleanup() {
  # -v removes anonymous volumes declared by the engine image as well as the
  # container itself. Self-hosted runners are long-lived, so leaving those
  # volumes behind turns repeated conformance runs into unbounded disk growth.
  docker rm -fv "${CONTAINER}" >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker run --detach \
  --name "${CONTAINER}" \
  --publish 127.0.0.1::8123 \
  --env CLICKHOUSE_USER=metis_ci \
  --env CLICKHOUSE_PASSWORD=metis_ci \
  "clickhouse/clickhouse-server:${CLICKHOUSE_VERSION}" >/dev/null

for _ in $(seq 1 30); do
  if docker exec "${CONTAINER}" wget -qO- http://localhost:8123/ping >/dev/null 2>&1; then
    CLICKHOUSE_PORT="$(metis_container_host_port "${CONTAINER}" 8123)"
    METIS_TEST_CLICKHOUSE_ADDRESS="localhost:${CLICKHOUSE_PORT}" \
    METIS_TEST_CLICKHOUSE_USERNAME="metis_ci" \
    METIS_TEST_CLICKHOUSE_PASSWORD="metis_ci" \
      make test-engine-clickhouse
    exit 0
  fi
  sleep 2
done

docker logs --tail 100 "${CONTAINER}" >&2
echo "ClickHouse did not become ready" >&2
exit 1
