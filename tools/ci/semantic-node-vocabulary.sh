#!/usr/bin/env bash
set -euo pipefail

production_files=()
# Release lifecycle terminology does not describe SemanticPlan nodes and is
# deliberately outside this vocabulary guard.
for root in app cmd/s2sbench ossie planner resolver; do
  while IFS= read -r -d '' file; do
    production_files+=("$file")
  done < <(find "$root" -type f -name '*.go' ! -name '*_test.go' \
    ! -path 'app/service/source/*' -print0 | sort -z)
done

if ((${#production_files[@]} == 0)); then
  echo "semantic node vocabulary guard: no production files found" >&2
  exit 2
fi

matches="$(grep -niH 'stage' "${production_files[@]}" || true)"
violations=""
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  if [[ "$line" =~ MetricDefinitionFilterStage|EffectiveMetricDefinitionFilterStage|definition-filter[[:space:]]stage|spec\.Stage|json:\"stage|\"stage\": ]]; then
    continue
  fi
  violations+="$line"$'\n'
done <<< "$matches"

if [[ -n "$violations" ]]; then
  echo "Semantic node vocabulary violation: retired Stage vocabulary remains in the semantic pipeline" >&2
  printf '%s' "$violations" >&2
  exit 1
fi

echo "Semantic node vocabulary contract: PASS"
