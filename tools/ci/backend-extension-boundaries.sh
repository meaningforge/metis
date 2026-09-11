#!/usr/bin/env bash
# Validates the package import boundaries that keep warehouse extensions
# separate from semantic compilation, query running, and each other.
set -euo pipefail

failed=0

module_path="$(go list -m -f '{{.Path}}')"

imports_of() {
  go list -f '{{join .Imports "\n"}}' "$1"
}

forbid_import() {
  local package="$1"
  local pattern="$2"
  local label="$3"
  local allowed="${4:-}"
  local imports

  if ! go list "$package" >/dev/null 2>&1; then
    return
  fi
  imports="$(imports_of "$package")"
  if [[ -n "$allowed" ]]; then
    imports="$(grep -Fxv "$allowed" <<<"$imports" || true)"
  fi
  if grep -Eq "$pattern" <<<"$imports"; then
    echo "Backend extension boundary violation: $label ($package)" >&2
    grep -E "$pattern" <<<"$imports" >&2
    failed=1
  fi
}

# The root registries are contracts/composition boundaries, never importers of
# concrete warehouse packages. Concrete Renderers and Drivers cannot depend on
# execution-runtime internals.
forbid_import \
  ./renderer \
  "^${module_path}/renderer/" \
  "root Renderer registry imports a concrete Renderer" \
  "${module_path}/renderer/sql"
forbid_import \
  ./renderer/sql \
  "^${module_path}/renderer$" \
  "shared SQL rendering imports the Renderer contract or registry"
forbid_import \
  ./renderer/sql \
  "^${module_path}/(manifest|planner|resolver|execution)(/|$)" \
  "shared SQL rendering imports semantic or execution state"
forbid_import \
  ./execution/backend \
  "^${module_path}/execution/backend/" \
  "root Backend registry imports a concrete Backend"
forbid_import \
  ./execution \
  "^${module_path}/execution/backend/" \
  "transitional execution root imports a concrete Backend"
forbid_import \
  ./execution/datasource \
  "^${module_path}/execution/(backend|driver)(/|$)" \
  "DataSource contracts import Backend or Driver"
forbid_import \
  ./execution/driver \
  "^${module_path}/execution/(backend|runner)(/|$)" \
  "Driver SPI imports Backend or Runner"
forbid_import \
  ./execution/runner \
  "^${module_path}/execution/backend/" \
  "query Runner imports a concrete Backend"
forbid_import \
  ./execution/runner \
  "^${module_path}/renderer(/|$)" \
  "query Runner imports Renderer state" \
  "${module_path}/renderer/sql"

# Application packages use the already assembled BackendRegistry and never
# select a concrete warehouse package. Enumerate all descendants so a newly
# added application package is covered without changing this guard.
while IFS= read -r package; do
  forbid_import \
    "./${package#"${module_path}/"}" \
    "^${module_path}/execution/backend/" \
    "application layer imports a concrete Backend"
done < <(go list -f '{{.ImportPath}}' ./app/... 2>/dev/null || true)

if [[ -d renderer ]]; then
  while IFS= read -r package; do
    case "$package" in
      "${module_path}/renderer/"*)
        forbid_import \
          "./${package#"${module_path}/"}" \
          "^${module_path}/execution(/|$)" \
          "Renderer imports execution"
        ;;
    esac
  done < <(go list -f '{{.ImportPath}}' ./renderer/... 2>/dev/null || true)
fi

if [[ -d execution/backend ]]; then
  while IFS= read -r package; do
    case "$package" in
      "${module_path}/execution/backend/"*)
        forbid_import \
          "./${package#"${module_path}/"}" \
          "^${module_path}/execution/runner(/|$)" \
          "concrete Backend imports query Runner internals"
        ;;
    esac
  done < <(go list -f '{{.ImportPath}}' ./execution/backend/... 2>/dev/null || true)
fi

if ((failed != 0)); then
  exit 1
fi

echo "Backend extension package boundaries: PASS"
