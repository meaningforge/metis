#!/usr/bin/env bash
# Capture/generate only. Database setup and business review remain explicit.
set -euo pipefail
umask 077
if [[ "$#" != 2 || ( "$1" != doris && "$1" != clickhouse ) || -z "$2" ]]; then
  echo 'usage: bash prepare.sh doris|clickhouse <new-work-directory>' >&2
  exit 2
fi
backend="$1"
example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
metis_bin="${METIS_BIN:-metis}"
if ! command -v "${metis_bin}" >/dev/null; then
  echo 'metis is unavailable; install it or set METIS_BIN to the built executable' >&2
  exit 2
fi
parent="$(cd "$(dirname "$2")" && pwd)"
work_dir="${parent}/$(basename "$2")"
# mkdir refuses existing files, directories and symlinks. No force/cleanup path.
mkdir -m 700 "${work_dir}"
for file in metis.yaml relations.json model-map.yaml model-reviewed.ossie.yaml query.json queries.json queries-filtered.json results.yaml; do
  cp "${example_dir}/${file}" "${work_dir}/${file}"
done
cp "${example_dir}/../datasources-${backend}.yaml" "${work_dir}/datasources.yaml"
"${metis_bin}" catalog inspect --config "${work_dir}/metis.yaml" --project sales \
  --data-source warehouse --relations "${work_dir}/relations.json" --output "${work_dir}/catalog.json"
"${metis_bin}" project init --catalog "${work_dir}/catalog.json" \
  --mapping "${work_dir}/model-map.yaml" --output "${work_dir}/candidate-sales"
"${metis_bin}" project validate --project sales --config "${work_dir}/candidate-sales/project.yaml"
"${metis_bin}" query compile --model "${work_dir}/candidate-sales/models/sales.ossie.yaml" \
  --dialect "${backend}" --metric order_rows --dimension region --output "${work_dir}/generated-count.json"
echo 'Candidate ready. Review the report and explicit business definition before adopting model-reviewed.ossie.yaml.'
