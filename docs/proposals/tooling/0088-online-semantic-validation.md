# RFC-0088: Online Semantic Validation

- **Status:** Draft
- **Created:** 2026-09-25
- **Last updated:** 2026-10-07
- **Scope:** Core authoring CLI, application validation services, bounded backend inspection

## Summary

Extend `metis project validate` with explicit `--online` and `--offline`
flags. Default validation remains offline. Online checks a selected query inventory
using production routing, the exact Renderer, physical column metadata and ordinary
non-executing EXPLAIN. Initial backends are Doris and ClickHouse.

Catalog capture stays `metis catalog inspect`; result expectations stay
`metis project test`. No additional runtime command or catalog-only mode is added.

## Commands and input

```sh
# Offline: semantic project manifest.
metis project validate --offline --project sales --config ./sales/project.yaml

# Online: deployment root registering the project and sources.
metis project validate --online --project sales --config ./metis.yaml \
  --queries ./queries.json --output ./validation.json
```

Flags are mutually exclusive. Omitting both selects offline. Online requires queries
and output; offline rejects them. Existing offline diagnostics/exit behavior remain,
with an additive `mode: offline` field in CLI output.

The inventory is strict JSON:

```json
{
  "schema_version": 1,
  "queries": [{
    "id": "revenue_by_region",
    "query": {
      "project": "sales",
      "model": "sales",
      "metrics": [{"name": "total_revenue"}],
      "dimensions": [{"name": "region"}]
    }
  }]
}
```

Require matching project, unique bounded IDs, 1–100 queries and at most 1 MiB.
Reject duplicate keys, unknown fields at every typed nesting level, and filter
numbers that lose precision in the existing float64 request representation.
Inspect raw input before custom unmarshallers; REST/MCP decoding stays unchanged.

## Authorization and preparation

The CLI explicitly uses trusted-local authoring. Embedders use
`LoadValidationRuntime` with the existing ProjectAuthorizer, asset visibility and
data-policy options. Default Project authorization fails closed.

Authorize project author and compile before deployment/source/registry reads.
Load only the selected semantic project. Invalid candidates or failed quality
thresholds stop before registry access. Source findings remain generic; reports
never expose source errors or hidden asset identities.

Apply author/compile asset visibility before route lookup. Prepare every query
with its model's applied production DataSource, exact Backend Renderer and existing
data-policy preflight. Any preparation failure starts zero online probes, including
queries prepared earlier. Each query has one source; model-level multi-source
placement is preserved. No execute grant is implied.

Secrets are lazy until the first authorized operation opens the production pool.
Runner owns admission, deadlines, cancellation and cleanup. Database credentials
govern metadata and EXPLAIN access; permissions are enforced again during SELECT.

## Catalog and engine evidence

Use the existing canonical resolved workload and transitive field-expression
closure, including join/filter/calendar obligations and bound policy columns.
Unresolvable dependencies prevent online work. Only exact qualified relations
are described, without row sampling. Require matched complete column lists.

Check required physical columns and direct declared/native type families.
Computed expressions receive dependency-existence and engine-planning checks,
not direct-column type comparisons. Unsupported mappings remain incomplete.
V1 does not certify decimal scale, temporal precision/timezone, nullability or
expression output types; detailed native evidence remains available through
catalog capture.

Optional driver `CompiledQueryValidator` uses ordinary EXPLAIN on original
compiled SQL. It never falls back to Execute, EXPLAIN ANALYZE, sample SELECT,
LIMIT rewrites or a raw SQL CLI. Discard bounded planning output.

Doris forwards ordered values through MySQL server-side parameter binding.
ClickHouse's positional database/sql binder expands values on the client, so
parameterized shapes are unsupported before catalog/secret access. A future
typed server-parameter implementation requires conformance evidence.
Missing optional capabilities remain unsupported.

Engine evidence is accepted or unsupported. Native operation errors are unavailable;
permission, missing-object and transient failures are never classified by message
parsing. Column/type mismatch requires complete metadata. EXPLAIN acceptance is
planning evidence at the observed time, not numerical correctness, guaranteed
SELECT permission, function side-effect freedom or a future database guarantee.

## Bounds, reports and exits

Five minutes per run, at most 30 seconds per open/inspection/probe, tightened by
deployment limits. Operations are sequential. Exact catalog limits remain
200 relations, 10,000 columns and 10 MiB per operation; inventories contain at most
100 cases and reports at most 10 MiB. Bounds fail rather than truncate. Close every
lease and stream on all paths.

Schema version 1 includes mode online, project, Metis version, pinned candidate
and inventory digests, offline_checked, complete, passed, and per-case ID,
compile_checked, catalog_checked, engine_prepared, backend, method, outcome and
bounded code. No global database snapshot is claimed. Inventory digest is SHA-256
of the owned decoded inventory serialized as JSON, excluding operational timing.

Cases are passed, failed, unsupported, unavailable or not_run. Completed mismatches
can be complete failures; unsupported/unavailable/not_run checks remain incomplete.
Shared semantic/execution errors retain stable codes. Private command categories:
source_dependency_mismatch, source_type_mismatch, source_type_unknown,
catalog_incomplete, backend_unsupported, parameter_validation_unsupported,
engine_validation_unsupported and validation_unavailable. These are private
authoring categories rather than new REST/MCP error codes.

Reports exclude SQL, parameters, source excerpts, physical identities, endpoints,
credentials and native errors. Publish owner-only output atomically to a fresh
path; refuse existing files/directories/symlinks. No overwrite option is included.
Exit 0: all selected checks passed. Exit 1: failed/incomplete validation.
Exit 2: command/input/report I/O failure. Failure reports never claim partial success.

## Acceptance

- Default/offline constructs no online dependencies. Help explains modes and the
  semantic-project versus deployment configuration distinction.
- Denied author/compile precedes configuration reads; invalid/quality-ineligible
  source and query preflight failures cause zero secret resolutions or opens.
- Asset/data-policy denial blocks probes; every query prepares before online work.
- Fake drivers prove exact Renderer/route, no Execute fallback, redaction, bounds
  and cleanup on errors/cancellation/deadlines.
- Real Doris/ClickHouse exercise actual CLI acceptance, missing columns/types,
  invalid query preflight and parameter support/unsupported cases.
- README, current contracts, extension guidance and runnable tutorial are updated.
- Mark Implemented after local and both native-engine gates pass.

## References

- [Offline authoring lifecycle](../../specs/semantic/asset-authoring-lifecycle.md)
- [Catalog authoring](../../specs/semantic/catalog-authoring.md)
- [Data policy preflight](../../specs/operations/data-access-policy.md)
- [Online validation contract](../../specs/semantic/online-validation.md)
