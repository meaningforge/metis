# RFC-0088: Online Semantic Validation

- **Status:** Draft
- **Owners:** TBD during review
- **Created:** 2026-09-25
- **Last updated:** 2026-09-25
- **Scope:** Core authoring CLI, application validation services, bounded backend inspection
- **Supersedes:** None
- **Numbering:** Proposed; retain 0086 for the closed federation proposal and 0087 for historical platform work.

## Summary

Add an explicit `metis validate-runtime` authoring command that verifies an
Ossie project's physical dependencies and compiles and prepares selected
semantic queries against their configured databases. Return a versioned report
that distinguishes offline validity, catalog compatibility, and engine acceptance.

This RFC proposes interfaces; none of the new commands below exist today.
The initial release covers Doris and ClickHouse. DuckDB inspection follows in
the existing optional CGO build and must report unsupported until implemented.

## Motivation

Current `s2s validate-project` performs deterministic loading, semantic validation,
and quality diagnostics. Those rules intentionally do not query databases.
`CompileService.Explain` renders SQL but does not run database EXPLAIN.
Production execution checks complete result contracts, but users currently
discover missing columns or dialect/version incompatibilities during queries.

The missing operation is a bounded authoring check between editing models and
deploying them. Passing it is evidence for a particular model, database, and
observed time; it is not a proof of numerical correctness or a future guarantee.

## Design

### Command and input

```sh
metis validate-runtime --config ./metis.yaml --project sales \
  --queries ./checks/queries.json --output ./reports/runtime-validation.json
```

`--config` uses the existing root runtime manifest, DataSource registry, secret
references, and exact Backend/Renderer binding. `--project` is required for this
authoring command. `--queries` is required for a full validation run and contains:

```json
{
  "schema_version": 1,
  "queries": [
    {
      "id": "revenue_by_region",
      "query": {
        "project": "sales",
        "model": "sales",
        "metrics": [{"name": "total_revenue"}],
        "dimensions": [{"name": "region"}]
      }
    }
  ]
}
```

Each query uses the existing `query.SemanticQuery` grammar and must match the
selected Project. IDs must be unique. Unknown fields, no cases, more than 100
cases, or a file larger than 1 MiB fail before any database access.
The tool does not enumerate every possible metric/dimension combination.
The inventory decoder validates allowed fields at every JSON object level,
including the nested semantic query, before unmarshalling it. This is a
command-specific input rule: the shared `SemanticQuery.UnmarshalJSON` currently
ignores most unknown fields and must not be tightened as a side effect of this
command, because existing REST/MCP inputs retain their contracts.

A separate explicit `--catalog-only` mode rejects `--queries` and omits
query preparation and engine probes. It inspects the declared direct physical
relations and fields of every semantic model in the selected Project, including
direct join and calendar fields. It does not claim coverage of query-specific
filter, computed-expression, or data-policy dependencies. Its report says
`scope: catalog_only`, lists the checked models and checkable declarations, and
cannot be presented as full runtime validation. A declaration whose physical
dependency cannot be determined is an incomplete check, not a silent omission.

### Validation stages

1. **Offline candidate:** call the shared source loader and return its validity
   and quality report. Invalid source or a failed configured quality threshold
   stops online work. Offline failures never resolve secrets or open connections.
   Before publishing source findings, apply the asset-visibility boundary;
   findings whose asset visibility cannot be established remain generic offline
   failures without asset identities or source excerpts.
2. **Placement and preparation:** resolve each complete semantic model to one
   applied DataSource. In a full run, prepare all requested query artifacts with
   that Backend's exact Renderer and the existing policy preflight before online
   probes begin. A preparation failure produces zero online query probes; it does
   not authorize partial workload execution. Catalog-only mode skips query
   preparation.
3. **Physical dependency inspection:** for a full run, use canonical resolved
   dependencies to identify exact physical relations and direct fields, including
   filter, join, calendar, and policy dependencies. In catalog-only mode, use
   only the declarations defined above. Read only selected catalog metadata.
4. **Engine acceptance:** in a full run, validate every prepared
   `CompiledQuery`, preserving SQL text and ordered parameters. Use a
   backend-owned, demonstrated non-executing preparation/EXPLAIN method. Never
   use EXPLAIN ANALYZE or substitute a SELECT sample. If a backend cannot
   validate a parameterized shape safely, report unsupported rather than
   interpolate values or execute the query.

The report distinguishes `catalog_checked`, `compile_checked`, and
`engine_prepared`. A successful EXPLAIN does not prove result values, complete
output type metadata, function side-effect freedom, or bounded engine planning
cost. Use an appropriately restricted validation database role and the existing
runtime deadlines. The backend support declaration documents precisely what
its preparation method checks for each supported server version.

### Catalog and driver boundary

Add optional, narrowly typed inspection capabilities to a driver lease rather
than adding semantic dependencies to `execution/driver`:

```go
// Proposed shapes; final package naming is decided in implementation review.
type CatalogInspector interface {
    DescribeRelations(context.Context, []RelationRef) ([]RelationInspectionResult, error)
}
type CompiledQueryValidator interface {
    ValidateCompiled(context.Context, *artifact.CompiledQuery) (ValidationEvidence, error)
}
```

`RelationRef` contains structured identifier parts, never free-form SQL.
`RelationInspectionResult` contains its requested reference and a closed outcome:
`found`, `not_found`, `unsupported`, `inconclusive`, or `unavailable`. `found`
contains `RelationMetadata` and an explicit statement of whether its column list
is complete. An absent column is a mismatch only when that list is complete;
otherwise it is unknown. A response must contain exactly one result per requested
reference; missing, duplicate, or extra results make the inspection inconclusive.
`RelationMetadata` contains relation identity/kind, column identity/native type,
precision/scale where available, and nullability as known/unknown. Metadata must
preserve quoted identifier case and distinguish catalog, schema, and database
according to the backend. Unsupported mappings are explicit evidence.

`ValidationEvidence` likewise has a closed outcome: `accepted`, `rejected`,
`unsupported`, `inconclusive`, or `unavailable`. Drivers classify native results
within their backend implementation and return bounded, typed reasons; callers
never parse native error strings. The `error` return is for an unclassified
operation failure and cannot by itself establish a missing object or a rejected
query. A failure to classify required evidence makes the run incomplete.

Runner owns lease acquisition, admission, effective timeout, secret resolution,
cancellation, observations, and cleanup for these operations. Driver SQL for
system catalogs and validation is a private backend implementation detail.
Do not fabricate a semantic query to execute metadata SQL. Do not expose a raw
SQL runner. Existing drivers remain source-compatible; missing optional
interfaces return an explicit capability failure.

The shared catalog contract is also reusable by catalog-assisted authoring.
It provides exact-object description only; schema crawling and sampling are
outside this first version.

### Type compatibility and coverage

Classify comparisons as `compatible`, `incompatible`, or `unknown`. A declared
Integer versus a physical string is incompatible; incomplete metadata is unknown,
not a success. Decimal precision, temporal precision/timezone, and unsupported
native types must retain evidence rather than collapse to a generic string.
Computed expression types are not compared directly to an arbitrary source
column type. Report catalog coverage only for facts that can actually be checked;
engine preparation supplies separate evidence for compiled expressions.

Source expressions whose physical dependencies cannot be enumerated are marked
unknown. A required unknown/unsupported check makes the requested scope
incomplete and non-successful. There is no implicit skip-to-green behavior.

### Authorization and deployment scope

This is initially a trusted local authoring command plus an embedding service,
with no REST/MCP registration. The CLI uses the explicit trusted-local Principal
mode; its database role determines access to catalog metadata.

An embedder must resolve and authorize Project `author` and `compile` before
loading source content or consulting semantic inventory, DataSource, Backend,
secrets, or catalog state. It must apply asset visibility and the existing data
policy preflight to every query. These permissions imply no `execute` grant.
Any future read-execution probe requires `execute` as well and a separate design
decision. Catalog reports are author/operator artifacts and must not enter Agent
discovery.

For catalog-only inspection, require visibility under both `author` and `compile`
for every model and authored asset needed to describe its direct physical
declarations before looking up its physical identity. If any required asset is
hidden, fail the requested scope as incomplete with a generic finding that is
indistinguishable from other unavailable catalog coverage; do not probe it,
name or count it in the report, or silently omit it from coverage. Full-run
source findings and query assets must satisfy the same two visibility actions
before their identities enter a report. No query exists in catalog-only mode,
so the query data-policy preflight is not evaluated and policy-only dependencies
receive no coverage claim. The trusted-local CLI explicitly selects the local
all-visible asset policy; an embedder uses its configured visibility policy.

The immutable candidate generation is pinned for the complete run. Sources remain
independent: multiple sources may be checked in one report, but every query has
exactly one resolved source. No cross-source query is introduced.

### Bounds, outcomes, and report

Default operation limits: 5 minutes total, 30 seconds per inspection/probe,
2 concurrent operations, 100 cases, 200 relations, 10,000 column records, and
10 MiB metadata/report budget. Effective runtime limits are the stricter of these
limits and deployment ceilings. Exceeding any bound is a failure, not truncation.
Close every lease and stream on success, failure, cancellation, and timeout.

Report schema version 1 includes candidate digest, Metis version, requested scope,
case-list digest, backend family and supported server-version evidence, stage
coverage, findings, and `complete`/`passed`. Operational timestamps and durations
are separated from stable findings and excluded from evidence digests. Online
metadata can change during or after the run; the report makes no global snapshot
claim across sources.

Closed check outcomes are `passed`, `failed`, `unsupported`, and `not_run`.
`not_run` always has a reason. Findings include stage, case/asset identity,
stable code, bounded source location, and one caller action. Suggested codes:

| Proposed code | Caller action | Meaning |
| --- | --- | --- |
| `SOURCE_DEPENDENCY_MISMATCH` | `CHANGE_MODEL` | The authored physical dependency differs from observed metadata. |
| `SOURCE_TYPE_MISMATCH` | `CHANGE_MODEL` | A direct authored type claim contradicts supported native metadata. |
| `ONLINE_VALIDATION_UNSUPPORTED` | `CHANGE_TARGET` | Required backend validation is unavailable. |
| `ONLINE_VALIDATION_INCONCLUSIVE` | `CHANGE_TARGET` | Required evidence could not be established. |
| `ONLINE_VALIDATION_UNAVAILABLE` | `CHANGE_TARGET` | Connection, privilege, deadline, or inspection prevented checking. |
| `GENERATED_QUERY_REJECTED` | `REPORT_DEFECT` | A valid supported compiled query is rejected by the configured engine. |

Only emit a mismatch when the driver can distinguish it reliably from permissions
or transient failure. Ambiguous native errors become inconclusive. Preserve
existing semantic error codes rather than reclassifying them by message parsing.
Register new codes in the central registry and transport projections if introduced.

Exit 0 means all required checks in the requested scope passed and quality is
eligible; exit 1 means a completed report contains a failed/incomplete check;
exit 2 means command/input/report I/O failure; interruption returns the conventional
signal exit. Missing credentials and unavailable databases cannot produce exit 0.

Reports omit credentials, endpoints, raw native errors, SQL, and parameter values.
Author-visible relation/field identities may be included. Write reports using a
temporary file and atomic rename with owner-only permissions; refuse an existing
output unless `--overwrite` is explicit. A failed run may publish a complete
failure report, but never a partial success report.

## Alternatives

- Extend `s2s validate-project` to connect automatically: mixes deterministic
  offline authoring with credentialed operations and changes existing behavior.
- Treat compile success as database validation: misses missing tables/columns,
  privileges, and engine-version differences.
- Execute every metric with LIMIT 0: an aggregate can still scan, and a rewritten
  query does not necessarily validate the original shape. No silent fallback.
- Build a second database client in CLI: duplicates credentials, pooling, and
  cancellation. Reuse production resource ownership and optional lease capabilities.

## Rollout and migration

1. Land typed catalog/probe evidence and bounded Runner operations with fake-driver
   tests; existing binaries and APIs retain their behavior.
2. Add Doris and ClickHouse implementations and declare tested version/method
   coverage. Expose `metis validate-runtime` only with explicit command help.
3. Add sample query inventories, failure examples, and an opt-in CI recipe.
4. Consider optional DuckDB coverage after both remote backends pass.

No automatic startup probe, publication service, source mutation, or runtime
replacement is added. The command can be removed from a CI job to roll back
adoption; it never mutates a running generation or warehouse data.

## Test and acceptance criteria

- Invalid model performs zero online operations. Denied author or compile access
  performs zero source loads, semantic inventory or DataSource/Backend reads,
  secret resolutions, lease acquisitions, and online operations.
- Catalog-only mode neither reveals hidden asset identities nor silently treats
  hidden or unresolvable declarations as complete; it excludes query-policy
  evidence from its coverage claim.
- Unknown inventory fields fail at every nesting level without changing existing
  REST/MCP decoding behavior.
- Missing relation/column, incompatible native type, unknown expression metadata,
  permission denial, and unsupported preparation produce distinguishable outcomes.
- Partial, duplicate, or extra per-relation responses cannot convert a missing
  object or column into a successful check; incomplete column metadata remains
  unknown, and raw driver errors never determine mismatch classifications.
- Bound parameters, unusual quoted identifiers, and Decimal/temporal values are
  preserved; no SQL interpolation or EXPLAIN ANALYZE path exists.
- Tests prove the online operation uses the same Backend/Renderer and resolved
  route as production compilation, including model-level multi-source placement.
- Timeout, queue cancellation, connection failure, and metadata overflow release
  permits and leave the same runtime usable for the next operation.
- Real Doris and ClickHouse gates execute successful and deliberately broken
  models, plus parameterized probes; compiler-only tests cannot satisfy this gate.
- Existing offline validation output and existing REST/MCP schemas are unchanged.
- Reports show which cases were checked and never claim project-wide numerical
  correctness from a finite query inventory.

## Documentation updates

Implementation updates the CLI README, runtime-bootstrap and public-contract
specifications, Renderer/Driver extension guidance, testing architecture, and a
new online-validation specification. Add this Draft to the Core RFC index only;
current specifications are updated when behavior is implemented.

## References and review decisions

- [Current offline authoring](../../specs/semantic/asset-authoring-lifecycle.md)
- [Current runtime binding](../../specs/operations/runtime-bootstrap.md)
- [Current data-policy preflight](../../specs/operations/data-access-policy.md)
- [Current driver SPI](../../../execution/driver/driver.go)
- [MetricFlow validation stages](https://docs.getdbt.com/docs/build/validation)

Before acceptance, confirm each backend's non-executing validation method and
supported parameter shapes with a small conformance spike, review type mapping,
and choose the final command flag/API spelling. These are implementation gates,
not evidence that the proposed capability is already delivered.
