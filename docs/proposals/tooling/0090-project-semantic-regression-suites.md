# RFC-0090: Project Semantic Regression Suites

- **Status:** Draft
- **Owners:** TBD during review
- **Created:** 2026-09-25
- **Last updated:** 2026-09-26
- **Scope:** Core offline tooling, governed execution services, project-owned CI assertions
- **Supersedes:** None
- **Numbering:** Proposed, subject to repository review.

## Summary

Let a semantic-project author store representative requests and independently
reviewed expectations alongside Ossie models, then run them in CI. Add an offline
compile mode and an explicit runtime result mode. Test the same query, comparison,
and attribution services that applications and Agents consume.

This is a deterministic project-testing interface. Existing engine conformance
continues to test Metis itself; S2SBench continues to evaluate Agent behavior.
The full command set, suite schema, and report below describe the proposed
end state.

Offline compilation, runtime `query_metrics` result suites, and JSON/JUnit output
are implemented as documented in the
[current regression-suite contract](../../specs/testing/project-compile-regression.md).
Comparison/attribution assertions and host-managed policy composition remain
proposals; this RFC remains Draft for those phases. The current contract is
authoritative for shipped syntax; later sections describe the proposed end state.

## Motivation

`metis project diff` reports semantic asset changes. Internal conformance verifies
Metis's supported semantics across engines. Neither gives an adopting team a
small, supported way to express "this revenue definition must still produce this
answer for these rows" or "this model change must not remove this output column."

Compilation success alone cannot detect a wrong denominator, join multiplicity,
time boundary, filter, or changed business definition. A previous query result
is not an independent oracle; automatic baseline approval can preserve mistakes.

## Design

### Commands and modes

```sh
# Proposed offline command: no connections or secret resolution.
metis project test --mode compile --project sales --config ./project.yaml \
  --suite ./checks/compile.yaml --dialect DORIS --output ./reports/compile.json

# Proposed local runtime command: configured services and bindings.
metis project test --mode runtime --config ./metis.yaml --project sales \
  --suite ./checks/results.yaml --output ./reports/results.json
```

Offline suites support `compile_sql` only and use an explicit compile dialect.
Runtime suites support `query_metrics`, `compare_metrics`, and `attribute_metric`.
A mismatched operation/mode is an input error, not a skip. Runtime mode accepts
no dialect/source override: Project/model placement selects its sole Backend.
The standalone runtime CLI tests semantic behavior with a fixed local Principal
holding only `semantic:execute` scope and the documented unrestricted data-policy
compatibility mode. It does not claim to exercise a deployment's custom
authorization or data policy.

Suites are versioned strict YAML/JSON. They contain IDs, existing operation request
DTOs, and a small closed assertion grammar. They are not a new query or analytics
IR. Reject unknown fields, duplicate IDs, implicit template evaluation, scripts,
shell hooks, embedded raw SQL, and remote includes.

`fixture.kind` is `externally_prepared` for a claimed frozen fixture or `live` for
mutable data. It is mandatory in runtime mode and absent in compile mode. Neither
value creates a transaction or proves that the database is immutable.

### Example result suite

```yaml
schema_version: 1
project: sales
fixture:
  id: sales-small-v1
  kind: externally_prepared
cases:
  - id: revenue_by_region
    operation: query_metrics
    request:
      query:
        project: sales
        model: sales
        metrics: [{name: total_revenue}]
        dimensions: [{name: region}]
    expect:
      outcome: success
      row_count: 2
      output_schema:
        columns:
          - {name: region, kind: dimension, datatype: String}
          - {name: total_revenue, kind: metric, datatype: Decimal}
      rows:
        mode: unordered
        values:
          - [{type: string, value: APAC}, {type: decimal, value: "120.00"}]
          - [{type: string, value: EMEA}, {type: decimal, value: "80.00"}]
```

`request` follows the existing operation DTO exactly. Each request's explicit
Project must match the suite/CLI Project. Output names are matched against the
actual compiler OutputSchema rather than inferred from metric labels. The example
describes the proposed test schema; implementers must validate all executable
examples against the current DTOs before shipping them.

`expect.outcome` is `success`, `semantic_error`, or `policy_denied`. The two
failure outcomes require a specific stable code plus `caller_action`. An
unrelated failure, timeout, or credential error never satisfies an expected
semantic error.

An expected policy denial is a separate assertion category: it passes only when
the exact denial code and `caller_action` match under a trusted host-supplied
Principal and policy composition. V1 admits `PROJECT_ACCESS_DENIED` and
`DATA_ACCESS_DENIED` for this category; an ordinary not-found response does not
by itself prove asset-visibility enforcement. Any other denial fails the case.
The offline and standalone runtime CLIs reject policy-denial expectations as
unsupported input rather than allowing an unrestricted local run to produce a
misleading result. Suites cannot supply identities, scopes, entitlements, or
policy predicates.

### First-release assertion contract

| Mode | Required capability |
| --- | --- |
| Compile | Exact output schema; expected warnings or semantic error; optional SQL/parameter snapshot |
| Metric result | Exact column schema, row count, and complete ordered rows or row multiset |
| Comparison | Typed period values, presence states, deltas and percentage states keyed by dimension tuple |
| Attribution | Typed total delta, supported component contributions, and evaluator reconciliation evidence |

All successful cases need at least one nontrivial assertion. Empty expectations
and merely accepting an HTTP success are invalid. Analytics expectation shapes
follow the existing typed result DTOs; no free-form JSONPath or arbitrary assertion
language is added. Provide named assertions for reconciliation and shared-grain
comparison, implemented from returned evidence rather than a second evaluator.

An exact column-schema assertion compares the complete ordered `OutputSchema`:
column name, semantic `kind`, declared `datatype`, and the presence and value of
`grain`. A missing optional field in an expected column asserts its absence in
the actual column; it is not a wildcard. The example above intentionally omits
`grain` because neither column has a time grain. A future subset-schema assertion
would need a separately named mode; V1 does not silently use partial matching.

Numeric values use tagged scalars. Integers remain exact; Decimals are parsed as
arbitrary-precision decimal strings. String "1", integer 1, and decimal 1 are
different value types. SQL null, absent tuple, and numeric zero are distinct.
Comparison's absent/null/zero states and undefined percentage are preserved.

Exact comparison is the default. Explicit per-column/per-field absolute and
relative decimal tolerances may be supplied, with nonnegative finite values:
`abs(actual - expected) <= max(abs_tol, rel_tol * abs(expected))`.
No implicit float conversion, global epsilon, NaN equality, or string-number coercion.

Unordered row mode compares multisets including duplicate multiplicities, not
sets. Tolerance-aware unordered rows require exact, unique key columns, with keys
checked on both expected and actual results before comparison. Otherwise reject
the case; do not greedily pair approximate rows. Ordered mode requires explicit
stable semantic ordering with a total tie-break, or a single-row result.

Date values retain calendar date identity. Zoned timestamps compare instants
at declared precision; local DateTime values do not acquire an inferred timezone.
Relative phrases such as "today" are not permitted in frozen expectations.
Exact period/filter boundaries follow the existing API semantics; the harness
must not alter inclusive/exclusive behavior to fit its fixtures.

### Execution and architectural ownership

Each test host loads one immutable candidate generation and prepares supported
services through bootstrap. The coordinator invokes the same authorized service
methods used by REST/MCP. Each operation retains normal visibility/data-policy
preflight, exact Renderer selection, full compiled artifact, Runner normalization,
limits, and complete-result semantics. No raw database executor is introduced.

There are two runtime compositions. The standalone CLI constructs a trusted local
Principal with `semantic:execute` scope and uses the explicit
`NoRestrictionDataAccessPolicy` compatibility mode. It verifies semantic results,
not tenant or row-policy behavior. A trusted embedder may instead invoke the
reusable suite coordinator with an authenticated Principal and a candidate
runtime assembled with its actual Project authorizer, asset-visibility policy,
and data-access adapter. The coordinator must preserve these host-owned adapters
and must not replace them with local defaults. Only this host-managed composition
may run policy-denial assertions. The suite and CLI cannot impersonate a caller
or select a policy. Report the composition as `local_unrestricted` or
`host_managed`; this label is provenance, not proof that any policy was enforced.

Each test host tightens a private copy of DataSource execution ceilings before
bootstrap; it never edits the input configuration or relaxes its limits. Suite and
case context deadlines bound whole analytical operations as well as each physical
query. Result-size assertions additionally bound total workflow evidence; exceeding
them fails a case. Request DTOs acquire no testing-only runtime controls.

Analytics suites call the complete `attribute_metric`/`compare_metrics` operations.
They never expose or replay internal attribution bundles as a new public compiler
interface. Partial workflow failures remain failures with no partial success data.

Offline compilation reuses CompileService. It does not need the online-validation
RFC to ship. Online result suites need execution services already present today,
not online catalog inspection. Authors may run RFC-0088 first for better diagnosis,
but a validation pass is not a substitute for result assertions.

Put reusable suite parsing/comparison and application orchestration in a production
tooling package with one-way imports into existing services. CLI packages remain
thin. Production tooling may not import `tests/**` or benchmark-owned packages.
Internal conformance stays authoritative for engine-neutral behavior and expected
engine results; extract only truly reusable value comparators when needed.

### Fixtures and independent expectations

V1 consumes externally prepared databases with documented fixture identity;
it does not execute DDL, seed rows, reset databases, or run suite-provided hooks.
CI provisions a dedicated small read-only test dataset before calling the CLI.
Checked-in examples provide separately reviewed setup instructions outside the
suite executor. Engine-specific physical setup must represent the same logical
rows and expectations.

`fixture.id` is an author declaration, not proof of a database snapshot. Reports
identify `fixture_verification: declared`; optional host-provided immutable snapshot
evidence is recorded only when supplied by a trusted integration. Shared live data
may change between cases and is explicitly classified as live validation, not a
reproducible regression run. A candidate-versus-baseline comparison has the same
requirement: both must use a fixed data reality, otherwise differences are inconclusive.

Expected results should come from a hand-calculated small dataset or a separately
reviewed reference query outside the runner. V1 has no command that overwrites
expectations with current output. A report can show a local observed-result diff,
but authors approve expected changes through ordinary Git review.

Important example fixtures include a one-to-many join, unmatched dimension row,
duplicate dimension tuple, SQL NULL, zero denominator, negative/refunded amount,
unequal comparison-period coverage, and an exact time-boundary event. These make
plausible errors observable rather than simply exercise successful code paths.

### Compilation evidence and baseline handling

Result expectations are the default correctness contract. Optional compiled-output
snapshots record the dialect, exact SQL, typed ordered parameters, and output schema
from a reviewed compilation. They detect rendering changes; a mismatch alone is
not proof of a semantic defect. Diagnostic summaries distinguish SQL-only changes
from schema/result changes. Do not normalize away identifiers, joins, predicates,
parameter values, or bindings with regexes.

Expected files record their schema and supported compiler version/dialect scope.
An engine upgrade, model change, or query edit can require deliberate baseline
review. A changed SQL snapshot must not automatically update result expectations.
No source or result baseline is modified by `metis project test`.

### Limits, reports, and CI semantics

V1 defaults to sequential cases, at most 100 cases, a 1 MiB suite, 10 MiB expected
results, 1,000 result rows and 1 MiB result bytes per case, a 30-second per-case
deadline, and a 10-minute suite deadline. Effective query limits only tighten
deployment ceilings; rejecting an oversized complete result cannot be turned into
truncation followed by a pass. A source/configuration failure marks remaining
affected cases `not_run` with a cause, never passed.

Versioned JSON is the primary report. A JUnit projection exposes the same statuses
for existing CI tools; it has no independent pass logic. Report per-case expected
and actual outcome, comparison category, and bounded differences, along with
candidate/suite/expectation digests, Metis/backend version evidence, fixture mode,
authorization composition, and scope. Do not report Principal identity, scopes,
or policy decisions beyond a case's expected/actual stable error code and
`caller_action`. Case ordering and typed comparison findings are deterministic
for the same inputs/results; timing/run metadata stays outside stable digests.

Statuses are `passed`, `failed`, and `not_run`. A required not-run case makes the
suite fail. There is no default skip, expected-failure quarantine, or auto-retry
that hides a first failure. Exit 0 requires every selected case to pass; 1 denotes
completed failing/incomplete evaluation; 2 denotes malformed input/report I/O;
interruptions use conventional signal exits.

Write reports privately and atomically, refusing existing output unless overwrite
is explicit. Default reports include case IDs, expected/actual schema types, row
counts and mismatch positions, not source data, SQL parameters, raw driver errors,
credentials, or endpoint strings. `--include-values` explicitly enables bounded
local result diffs and must be opt-in in CI; artifacts with data inherit the
dataset's handling rules. Assertion values are never metric labels or traces.

## Alternatives

- Reuse Agent S2SBench runs: adds model/token nondeterminism to a deterministic
  project test and conflates Agent competence with metric correctness.
- Rely only on SQL snapshots: equivalent SQL changes create noise and wrong
  business results can remain undetected. Result assertions are primary.
- Compare candidate results only to the previous compiler: both can share the
  same bug. Keep independently reviewed expectations.
- Add a full assertion language or data-quality scheduler: unnecessary for the
  first use cases. Use a closed assertion set and existing CI orchestration.

## Rollout and migration

1. Ship strict suite parsing, tagged value comparison, and offline compile mode.
2. Ship runtime metric result mode using existing services, plus JSON/JUnit output
   and Doris/ClickHouse examples against externally provisioned fixtures.
3. Ship comparison and attribution assertions through their public services.
   Until supported, those operations fail explicitly; do not claim the full RFC
   implemented after only metric tests ship.
4. Add host-managed policy-denial assertions through the reusable coordinator,
   with trusted Principal and policy injection. Until then, the standalone CLI
   rejects policy-denial expectations and no policy coverage is claimed.
5. Add an optional affected-case suggestion based on existing semantic diffs only
   after full-suite execution is dependable. Initially all cases run.

Suites are additive files. Existing models, REST/MCP interfaces, internal benchmark
artifacts, and engine conformance retain their contracts. Pin versions in CI;
unsupported suite versions fail explicitly. Disabling a CI job reverses adoption
without changing model or runtime state.

## Test and acceptance criteria

- Detect wrong SUM, missing filter, duplicated join rows, removed column, changed
  datatype, changed time boundary, and wrong ratio denominator on frozen fixtures.
- Compare Decimal and large Integer values exactly; distinguish absence/null/zero,
  handle duplicate rows, and reject ambiguous approximate unordered matching.
- Verify comparison delta/percentage states and attribution reconciliation through
  complete production workflows, including zero/negative and unsupported cases.
- An unexpected policy denial, schema mismatch, timeout, connection failure,
  partial stream, or oversized result cannot yield a passing case or leak
  permits/data. An expected policy denial passes only with its exact stable code
  and `caller_action` under a trusted host-managed Principal and policy runtime.
- Standalone CLI reports identify unrestricted local policy composition and reject
  policy-denial expectations; host-managed reports identify their composition
  without exposing Principal attributes or policy decisions.
- Exact column-schema assertions detect changed semantic `kind` and `grain` as
  well as column names, datatypes, order, and optional-field presence.
- Identical fixtures on Doris and ClickHouse use shared logical expectations;
  optional DuckDB uses the same assertion contract in its supported build.
- Run the CLI outside a Metis checkout; no `tests/**` imports or relative repository
  runtime paths are required.
- Existing offline `metis` commands remain offline, and runtime tests never accept a source
  override, raw SQL, or suite-supplied policy/Principal.
- JSON and JUnit report identical pass/fail/not-run totals, and missing fixture or
  unsupported capability produces a nonzero exit.
- Every shipped sample request validates against actual operation DTOs and every
  numerical expectation has a documented independent derivation.

## Documentation updates

Add project regression suite and CI guides; update the public CLI contract,
source-authoring guidance, and testing architecture to distinguish project tests,
internal engine conformance, and Agent benchmarks. Implementation documentation
must show a failed business regression and its reviewed fix, not only green runs.

## References and review decisions

- [Source comparison](../../specs/semantic/asset-authoring-lifecycle.md)
- [Public query and analytics contracts](../../specs/public-contract.md)
- [Testing ownership](../../specs/testing/architecture.md)
- [S2SBench's separate contract](../../specs/testing/s2sbench-cli.md)
- [Production metric query service](../../../app/service/semantic/query_metrics.go)

Before acceptance, settle the minimal analytics expectation DTOs, report redaction
defaults, and supported exact/tolerant scalar types. Keep fixture provisioning
outside the runner and avoid creating another analytic evaluator or SQL executor.
