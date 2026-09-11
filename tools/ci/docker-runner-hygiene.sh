#!/usr/bin/env bash
set -euo pipefail

SOFT_MIN_GB="${METIS_CI_DOCKER_RECLAIM_BELOW_GB:-20}"
HARD_MIN_GB="${METIS_CI_DOCKER_MIN_FREE_GB:-12}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required on this self-hosted runner" >&2
  exit 1
fi

docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)"
probe_path="${docker_root:-/}"
if [[ ! -e "${probe_path}" ]]; then
  probe_path=/
fi

available_kb() {
  df -Pk "${probe_path}" | awk 'NR == 2 {print $4}'
}

print_state() {
  echo "Docker root: ${docker_root:-unknown}"
  df -h "${probe_path}" || true
  docker system df || true
}

print_state

soft_kb=$((SOFT_MIN_GB * 1024 * 1024))
hard_kb=$((HARD_MIN_GB * 1024 * 1024))
free_kb="$(available_kb)"

if (( free_kb < soft_kb )); then
  echo "Docker filesystem has less than ${SOFT_MIN_GB} GiB free; reclaiming stale CI artifacts."
  # These commands never remove running containers. Unused images and old
  # build cache are safe to recreate on a dedicated CI runner.
  docker container prune -f >/dev/null || true
  docker image prune -af --filter 'until=24h' >/dev/null || true
  docker builder prune -af --filter 'until=24h' >/dev/null || true
  free_kb="$(available_kb)"
  print_state
fi

if (( free_kb < hard_kb )); then
  echo "Docker filesystem still has less than ${HARD_MIN_GB} GiB free after safe reclamation." >&2
  echo "Refusing to start a real-engine/container job on a disk-starved runner." >&2
  exit 1
fi
