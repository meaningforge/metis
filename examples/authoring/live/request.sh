#!/usr/bin/env bash
set -euo pipefail
if [[ "$#" != 1 || ! "$1" =~ ^http://(127\.0\.0\.1|localhost):[0-9]+$ ]]; then
  echo 'usage: bash request.sh http://127.0.0.1:<port>' >&2
  exit 2
fi
if [[ -z "${METIS_API_KEY:-}" ]]; then
  echo 'METIS_API_KEY is required' >&2
  exit 2
fi
example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Headers travel on stdin rather than putting the bearer token in curl argv.
# Do not follow redirects or accept a remote endpoint in this local walkthrough.
printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "${METIS_API_KEY}" |
  curl --disable --noproxy '*' --fail --silent --show-error --max-time 30 --header @- \
    --data-binary "@${example_dir}/query.json" "$1/v1/query-metrics"
