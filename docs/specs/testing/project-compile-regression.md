# Project compile regression suite

`metis project test --mode compile` checks a complete semantic project's
compilation contract without connecting to a database, resolving secrets, or
running SQL. It uses the project's normal source loader and `CompileService`
with an explicit Renderer dialect. It does not test result values, database
permissions, or a deployment's host authorization policy. Runtime result suites
and JUnit output proposed by RFC-0090 are not implemented.

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
