#!/usr/bin/env bash
set -euo pipefail

planner_files=()
while IFS= read -r -d '' file; do
  planner_files+=("$file")
done < <(find planner -type f -name '*.go' ! -name '*_test.go' -print0 | sort -z)

if ((${#planner_files[@]} == 0)); then
  echo "metric evaluation authority guard: no planner production files found" >&2
  exit 2
fi

metric_evaluation_builder=planner/evaluation/metric_evaluation_plan.go
planner_orchestrator=planner/planner.go

failed=0

forbid_all() {
  local label="$1"
  local pattern="$2"
  local matches
  matches="$(grep -nH -E "$pattern" "${planner_files[@]}" || true)"
  if [[ -n "$matches" ]]; then
    echo "Metric evaluation authority violation: $label" >&2
    echo "$matches" >&2
    failed=1
  fi
}

allow_only() {
  local label="$1"
  local pattern="$2"
  shift 2
  local matches disallowed file allowed
  matches="$(grep -nH -E "$pattern" "${planner_files[@]}" || true)"
  [[ -z "$matches" ]] && return 0

  disallowed=""
  while IFS= read -r line; do
    file="${line%%:*}"
    allowed=0
    for candidate in "$@"; do
      if [[ "$file" == "$candidate" ]]; then
        allowed=1
        break
      fi
    done
    if ((allowed == 0)); then
      disallowed+="$line"$'\n'
    fi
  done <<< "$matches"

  if [[ -n "$disallowed" ]]; then
    echo "Metric evaluation authority violation: $label" >&2
    printf '%s' "$disallowed" >&2
    failed=1
  fi
}

# Once MetricEvaluationPlan exists, planner code may not ask the catalog graph
# to classify a metric as a source/leaf again.
forbid_all "planner must not reopen metric source classification" '\.MetricSources\('
allow_only \
  "catalog metric edges may only enter MetricEvaluationPlan at its builder boundary" \
  'dependency\.Metrics' \
  "$metric_evaluation_builder"
forbid_all "planner must not traverse the catalog metric dependency graph below MetricEvaluationPlan" 'MetricDependencyGraph'

# ResolvedSemanticQuery.EvaluationMetrics is transitional input to the plan
# builder. No other planner file may consume the collection after the single
# query-scoped MetricEvaluationPlan has been built and validated.
allow_only \
  "EvaluationMetrics is only a MetricEvaluationPlan builder input" \
  'EvaluationMetrics' \
  "$metric_evaluation_builder"

# Root Planner orchestration builds exactly one query-scoped evaluation plan
# and transfers it explicitly to builder. Builder and lower stages must not
# recreate a second authority path.
allow_only \
  "MetricEvaluationPlan may only be built by root Planner orchestration" \
  'BuildMetricEvaluationPlan\(' \
  "$metric_evaluation_builder" \
  "$planner_orchestrator"
forbid_all \
  "transitional metric evaluation construction wrappers are closed" \
  'metricEvaluationConstruction|buildCanonicalMetricEvaluationConstruction'

sql_plan_matches="$(find planner/conversion -type f -name '*.go' ! -name '*_test.go' -print0 | xargs -0 grep -nH -E 'MetricEvaluationPlan|MetricEvaluationNode' || true)"
if [[ -n "$sql_plan_matches" ]]; then
  echo "Metric evaluation authority violation: SQLPlan lowering may not consume MetricEvaluationPlan state" >&2
  echo "$sql_plan_matches" >&2
  failed=1
fi

# Advanced metric kind/spec interpretation happens exactly once while building
# MetricEvaluationPlan. Lower layers consume MetricEvaluationNode.Kind and its
# typed spec rather than reopening Ossie metric extensions.
for helper in CumulativeSpec TimeOffsetSpec OffsetToGrainSpec ConversionSpec SemiAdditiveSpec; do
  allow_only \
    "ossie.${helper} may only classify MetricEvaluationPlan nodes" \
    "ossie\\.${helper}\\(" \
    "$metric_evaluation_builder"
done

# Canonical time binding is already carried by MetricEvaluationNode. Re-reading
# it from the model below the plan boundary would create another semantic author.
allow_only \
  "metric time binding may not be rediscovered below MetricEvaluationPlan" \
  'ossie\.MetricTimeBinding\(' \
  "$metric_evaluation_builder"

if ((failed != 0)); then
  exit 1
fi

echo "Metric evaluation authority contract: PASS"
