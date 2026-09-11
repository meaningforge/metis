#!/usr/bin/env bash

# Shared process-local Docker helpers for real-engine conformance launchers.
# Engine scripts retain their own readiness contract; this file owns only
# collision-free resource identity and dynamically published host ports.

metis_container_run_id() {
  local raw
  raw="${METIS_ENGINE_RUN_ID:-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$$-${RANDOM}}"
  printf '%s' "${raw}" | tr -c '[:alnum:]_.-' '-'
}

metis_container_host_port() {
  local container="$1"
  local container_port="$2"
  local binding
  binding="$(docker port "${container}" "${container_port}/tcp" | head -n 1)"
  local port="${binding##*:}"
  if [[ ! "${port}" =~ ^[0-9]+$ ]]; then
    echo "cannot resolve published port ${container_port} for ${container}" >&2
    return 1
  fi
  printf '%s\n' "${port}"
}

metis_container_network_addresses() {
  local network="$1"
  local gateway
  gateway="$(docker network inspect "${network}" --format '{{(index .IPAM.Config 0).Gateway}}')"
  local prefix="${gateway%.*}"
  if [[ -z "${prefix}" || "${prefix}" == "${gateway}" ]]; then
    echo "cannot derive IPv4 addresses from Docker network gateway" >&2
    return 1
  fi
  printf '%s.2 %s.3\n' "${prefix}" "${prefix}"
}
