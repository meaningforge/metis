#!/usr/bin/env bash
set -euo pipefail
umask 077
if [[ "$#" != 2 || ! -f "$1" || -z "$2" ]]; then
  echo 'usage: bash prepare.sh <existing-duckdb-file> <new-work-directory>' >&2
  exit 2
fi
example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
metis_bin="${METIS_BIN:-metis}"
if ! command -v "${metis_bin}" >/dev/null; then
  echo 'install a DuckDB-enabled metis or set METIS_BIN to its absolute path' >&2
  exit 2
fi
export METIS_DUCKDB_PATH="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
parent="$(cd "$(dirname "$2")" && pwd)"
work_dir="${parent}/$(basename "$2")"
mkdir -m 700 "${work_dir}"
for file in metis.yaml datasources.yaml relations.json model-reviewed.ossie.yaml; do
  cp "${example_dir}/${file}" "${work_dir}/${file}"
done
# Shared explicit mapping, queries and independently reviewed expectations.
for file in model-map.yaml queries.json queries-filtered.json results.yaml; do
  cp "${example_dir}/../live/${file}" "${work_dir}/${file}"
done
"${metis_bin}" catalog inspect --config "${work_dir}/metis.yaml" --project sales \
  --data-source warehouse --relations "${work_dir}/relations.json" --output "${work_dir}/catalog.json"
"${metis_bin}" semantic init --catalog "${work_dir}/catalog.json" \
  --mapping "${work_dir}/model-map.yaml" --output "${work_dir}/candidate-sales"
"${metis_bin}" semantic validate --offline --project sales --config "${work_dir}/candidate-sales/project.yaml"
echo 'Candidate ready. Review mappings and business semantics before adopting model-reviewed.ossie.yaml.'
