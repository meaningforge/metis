#!/usr/bin/env bash
set -euo pipefail

DORIS_VERSION="3.0.8"
source "$(dirname "${BASH_SOURCE[0]}")/../container/runtime.sh"
RUN_ID="$(metis_container_run_id)"
FE_CONTAINER="metis-doris-conformance-fe-${RUN_ID}"
BE_CONTAINER="metis-doris-conformance-be-${RUN_ID}"
NETWORK="metis-doris-conformance-${RUN_ID}"

cleanup() {
  # -v removes anonymous volumes declared by the engine images together with
  # the containers. Long-lived self-hosted runners otherwise accumulate those
  # volumes across repeated real-engine conformance runs.
  docker rm -fv "${BE_CONTAINER}" "${FE_CONTAINER}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK}" >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker network create "${NETWORK}" >/dev/null
read -r FE_IP BE_IP <<< "$(metis_container_network_addresses "${NETWORK}")"

FE_ARGS=(
  --detach
  --name "${FE_CONTAINER}"
  --network "${NETWORK}"
  --ip "${FE_IP}"
  --publish 127.0.0.1::9030
  --env "FE_SERVERS=fe1:${FE_IP}:9010"
  --env FE_ID=1
  --env "JACOCO_COVERAGE_OPT=-Xmx2G -Xms2G"
)
BE_ARGS=(
  --detach
  --name "${BE_CONTAINER}"
  --network "${NETWORK}"
  --ip "${BE_IP}"
  --ulimit nofile=65536:65536
  --env "FE_SERVERS=fe1:${FE_IP}:9010"
  --env "BE_ADDR=${BE_IP}:9050"
)
docker run "${FE_ARGS[@]}" "apache/doris:fe-${DORIS_VERSION}" >/dev/null
docker run "${BE_ARGS[@]}" "apache/doris:be-${DORIS_VERSION}" >/dev/null
DORIS_PORT="$(metis_container_host_port "${FE_CONTAINER}" 9030)"

for _ in $(seq 1 180); do
  if docker exec "${FE_CONTAINER}" mysql -uroot -h127.0.0.1 -P9030 -e 'SELECT 1' >/dev/null 2>&1; then
    # The image entrypoint can report the BE container ready before doris_be
    # has opened its heartbeat service and reported a usable storage path.
    # Starting Go tests at FE-only readiness makes slow Docker Desktop/ARM
    # startups exhaust each test's backend wait independently. SHOW BACKENDS
    # is FE-local and remains queryable while no BE is available; field 10 is
    # Alive and field 16 is TotalCapacity in batch output.
    if ! docker exec "${FE_CONTAINER}" mysql -N -B -uroot -h127.0.0.1 -P9030 -e 'SHOW BACKENDS' 2>/dev/null |
      awk -F '\t' '$10 == "true" && $16 !~ /^0([.]0+)?([[:space:]]|$)/ { ready = 1 } END { exit !ready }'; then
      sleep 2
      continue
    fi
    METIS_TEST_DORIS_HOST="127.0.0.1" \
      METIS_TEST_DORIS_PORT="${DORIS_PORT}" \
      make test-engine-doris
    exit 0
  fi
  sleep 2
done

docker logs --tail 100 "${FE_CONTAINER}" >&2
docker logs --tail 100 "${BE_CONTAINER}" >&2
echo "Doris FE/BE did not become ready" >&2
exit 1
