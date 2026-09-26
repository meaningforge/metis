# Project regression suites

## Purpose and ownership

`metis project test` is developer tooling for a project's authored business
definitions, intended for local use and the author's existing CI. It is not a
general test framework or an engine capability. Internal conformance tests
verify Metis itself; host integration tests verify custom authorization; neither
is replaced by project suites. See [testing ownership](architecture.md).

The CLI stays a thin entry point into `app/tooling/regression`, which reuses
existing services. Fixture preparation, environments, scheduling, and permission
matrices remain external. Comparison/attribution snapshots and host-managed
policy assertions are outside the scoped v1, not committed follow-up phases.

## Offline compilation

`metis project test --mode compile` checks a complete semantic project's
compilation contract without connecting to a database, resolving secrets, or
running SQL. It uses the project's normal source loader and `CompileService`
with an explicit Renderer dialect. It does not test result values, database
permissions, or a deployment's host authorization policy. Runtime metric-result
suites are available separately with `--mode runtime`.

```sh
metis project test --mode compile --project demo \
  --config ./examples/demo/project.yaml \
  --suite ./examples/demo/checks/compile.yaml \
  --dialect DORIS --output ./compile-report.json
```

The suite is a strict, versioned YAML or JSON document. Unknown fields at any
level, duplicate keys or case IDs, multiple documents, YAML aliases, unsupported
operations, or more than 100 cases are rejected. The suite is limited to 1 MiB.
The CLI Project and every request Project must match the suite Project.

```yaml
schema_version: 1
project: demo
cases:
  - id: revenue_by_region
    operation: compile_sql
    request:
      query:
        project: demo
        model: sales
        metrics: [{name: total_revenue}]
        dimensions: [{name: region}]
    expect:
      outcome: success
      output_schema:
        columns:
          - {name: region, kind: dimension, datatype: String}
          - {name: total_revenue, kind: metric, datatype: Decimal}
      warnings: []
```

Every successful case requires the complete ordered `output_schema`. Each
column asserts its name, semantic kind, declared datatype, and the exact
presence and value of `grain`. Optional `warnings` asserts the complete ordered
warning list; write `warnings: []` to assert no warnings. An optional
`sql_render_result` requires `compiler_version` matching the Metis build and
asserts the exact `dialect`, SQL text, and ordered typed
parameters. Parameter entries have `name` (optional), `type` (`null`, `string`,
`bool`, `integer`, `float`, `decimal`, or `bytes`), and `value`. Decimal values
are exact strings and byte values are base64 strings. SQL snapshots detect
rendering changes but do not establish numerical correctness.

An expected semantic failure uses `expect: {outcome: semantic_error, code:
METRIC_NOT_FOUND, caller_action: CHANGE_REQUEST}`. Both the stable code and
registered caller action must match. Policy-denial expectations and unknown or
internal error codes are not supported by this local compile mode. A timeout,
candidate load failure, or other incomplete operation never satisfies an
expected semantic error.

The command writes a versioned JSON report with case-level `passed`, `failed`,
and `not_run` statuses, candidate/suite/expectation digests, and bounded
diagnostic categories. Schema mismatches include column positions and up to 100
expected and actual column descriptions. It does not include SQL text, parameter values, source
data, credentials, or raw driver errors. Output files are owner-only and
created atomically; an existing path is refused unless `--overwrite` is set.
Exit 0 means every case passed, 1 means a completed failing or incomplete
suite, and 2 means invalid input or report I/O failure. The whole suite has a
10-minute deadline and each case has a 30-second deadline.

## Runtime metric results

`--mode runtime` takes a **deployment** configuration, not a project manifest.
It calls the normal `query_metrics` service with the Project/model's configured
Backend. No dialect, connection, SQL, credential, or policy override is accepted
in the suite. Use a dedicated read-only database account with access to the fixture
tables. Database permissions still apply. The local runtime uses a fixed local
Principal with `semantic:execute` and unrestricted compatibility policies; it
does not validate custom host authorization or tenant isolation.

```sh
metis project test --mode runtime --project regression \
  --config ./examples/regression/metis-doris.yaml \
  --suite ./examples/regression/results.yaml \
  --output ./results.json --junit-output ./results.xml
```

See [the Doris/ClickHouse fixture example](../../../examples/regression/README.md)
for setup. The suite runner never creates, resets, or seeds a database. Runtime
suites require `fixture: {id: ..., kind: externally_prepared}` (claimed frozen
data) or `kind: live` (mutable data). Reports record `fixture_verification:
declared_only`: neither choice proves immutability or opens a snapshot transaction.

Runtime suites support only `operation: query_metrics`. Every successful case
requires `output_schema`, `row_count`, and complete `rows`; SQL snapshots are not
allowed. Comparison and attribution suites, host-managed policy testing, and
baseline approval are outside the scoped v1 and are not supported.

Rows use tagged cells. `integer`, `decimal`, and `float` values must be quoted
finite decimal strings (no binary floating-point conversion). `string`, `date`,
`time`, `datetime`, and `datetime_tz` also use strings; `bool` uses a boolean;
`{type: null}` is distinct from zero, false, and empty text. Tags must match the
declared schema. Dates use `YYYY-MM-DD`, times `HH:MM:SS[.fraction]`, local datetimes
`YYYY-MM-DDTHH:MM:SS[.fraction]`, and zoned datetimes RFC3339. Zoned values compare
as UTC instants; local values receive no timezone inference. Numeric scale alone
does not affect equality. Numeric literals are limited to 256 characters and a
three-digit exponent; nonfinite and unsupported values fail closed.

`rows.mode: unordered` compares exact multisets, including duplicate counts.
Optional `tolerances: {total_revenue: {abs: "0.01", rel: "0.001"}}` applies only to
numeric non-key columns using `abs(actual - expected) <= max(abs, rel * abs(expected))`.
Unordered tolerance matching requires exact, unique `key_columns`; ambiguous
duplicates fail instead of being greedily paired. `ordered` compares by position;
multiple rows require unique `key_columns` and an explicit `order_by` covering
every key using its output name. Key uniqueness is checked on both sides.

The private runtime tightens, never relaxes, configured limits to at most 1,000
rows, 1 MiB, and 30 seconds per query. The suite still has a 10-minute deadline.
Full normalized rows are additionally size-checked; limits and incomplete results
fail, not truncate-to-pass. Runtime load failures leave cases `not_run`; query
outages, cancellation, schema/normalization failures and limits cannot satisfy
expected semantic errors. Reports include Backend types but no endpoints or row
values. Differences identify bounded row/column positions, not data.

Both modes accept optional `--junit-output`. JSON remains the canonical report;
JUnit maps failed cases to failures and not-run cases to errors, never successful
skips. Outputs are independently atomic, not a two-file transaction. Exit 2 can
therefore leave a valid JSON report if writing JUnit fails. Use distinct paths;
existing outputs require `--overwrite`. CI must check the command exit status.

## Investigating a business regression

The [runtime example](../../../examples/regression/README.md) deliberately has a
small independently calculated answer: APAC has amounts 50 and 70, so its revenue
is 120; EMEA has amount 80, so its revenue is 80. Prepare this fixture externally
and keep it unchanged throughout the check.

If an author accidentally changes `total_revenue` from `SUM(orders.amount)` to
`AVG(orders.amount)`, the model can still compile with the same Decimal schema.
The runtime suite should fail because APAC becomes 60 rather than 120. A compile
schema check alone cannot establish that the business definition is correct.
The default report identifies a result mismatch without recording either value.

Investigate the model diff and fixture rather than copying current output into
the expectation. When the reviewed definition is total revenue, restore the
`SUM` expression and rerun with a fresh report path (or explicit `--overwrite`).
Keep the independently reviewed 120/80 expectations unchanged. If the business
definition intentionally changes instead, review the new model and hand-derived
expectations together in Git. There is no automatic baseline-approval command.
