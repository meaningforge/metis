# RFC-0090: Project Semantic Regression Suites

- **Status:** Draft — scope revision; scoped v1 implemented
- **Owners:** TBD during review
- **Created:** 2026-09-25
- **Last updated:** 2026-09-26
- **Scope:** Project-author developer tooling for local and CI assertions
- **Supersedes:** None

## Decision and scope revision

Keep `metis project test` as a small developer-tooling command for semantic
project authors. It answers: **does my authored model still produce the reviewed
business answer for this fixture?** It is not an engine capability, a general
testing framework, or a replacement for Metis's internal correctness tests.

The original proposal also planned comparison/attribution snapshots and
host-managed policy-denial assertions. Those extensions are removed from this
RFC's required scope, not claimed as implemented. There is no commitment to add
them merely to complete this RFC. They require a concrete project-author use
case and a separate scope review before implementation.

The scoped v1 is already implemented by PRs #14 and #15: offline compilation,
runtime metric-result assertions, and private JSON/JUnit reports. This revision
changes documented scope; it does not remove shipped commands, change suite
schema version 1, or alter query/analytics services. The
[shipped suite contract](../../specs/testing/project-compile-regression.md) is
authoritative for syntax and execution behavior.

## Why a CLI command?

A project author can validate model structure and inspect semantic changes but
still accidentally change revenue, a filter, or a join's multiplicity. A small
checked-in suite with independently reviewed answers catches business regressions
locally and in existing CI, without requiring a running Metis server or another
installation just to invoke the existing services.

| Question | Owner |
| --- | --- |
| Does an authored metric match this project's business expectation? | Project-owned suite via `metis project test` |
| Does Metis implement a semantic operation correctly across engines? | Internal conformance and real-engine harnesses |
| Does a deployment enforce identities and data policies? | Host/application integration tests |
| Can an Agent complete an analytical task? | S2SBench |

Project suites do not prove engine support, tenant isolation, or Agent quality.
Internal tests remain authoritative for their distinct responsibilities.

## Included v1 capabilities

```sh
metis project test --mode compile --project demo \
  --config ./examples/demo/project.yaml \
  --suite ./examples/demo/checks/compile.yaml \
  --dialect DORIS --output ./compile.json

metis project test --mode runtime --project regression \
  --config ./examples/regression/metis-doris.yaml \
  --suite ./examples/regression/results.yaml \
  --output ./results.json --junit-output ./results.xml
```

- Compile mode supports `compile_sql`: exact output schema, optional stable
  warnings, expected semantic errors, and optional version/dialect-specific
  SQL/parameter snapshots. It does not connect to a database or resolve secrets.
- Runtime mode supports `query_metrics`: complete output schema, row count, and
  exact ordered rows or an unordered multiset. Tagged numeric values retain
  precision; null, zero, and text remain distinct. Explicit finite decimal
  tolerances and unique-key requirements follow the shipped contract.
- Runtime fixtures are externally prepared. `fixture.id` and `fixture.kind`
  declare identity and whether data is claimed frozen or live. The report's
  `fixture_verification: declared_only` neither proves immutability nor establishes
  a snapshot transaction.
- Runtime uses a fixed local Principal with `semantic:execute` and unrestricted
  compatibility policies. Database permissions still apply. This tests semantic
  answers, not a host's custom authorization.
- Both modes produce JSON and optional JUnit. Incomplete runs never pass;
  diagnostics exclude result values, SQL/parameter values, credentials, and raw
  driver errors. Reports never update expectations.

Suites use strict versioned YAML/JSON. Unknown fields, unsupported operations,
duplicate IDs, hooks, identities, scopes, policy predicates, and physical
overrides are rejected, not treated as extension mechanisms. Standalone suites
reject policy-denial expectations.

## Architectural boundary

The command in `cmd/metis` parses arguments, invokes tooling, writes reports,
and returns an exit code. `app/tooling/regression` owns suite validation,
comparison, and orchestration with one-way imports into existing services.
It uses normal source/bootstrap, CompileService, and QueryMetricsService paths,
not a parallel query executor or metric evaluator.

Compiler, planner, renderer, and execution services acquire no suite grammar,
assertion logic, or testing-only request fields. Production tooling does not
import `tests/**` or benchmark packages. Generic bootstrap execution ceilings
can tighten private runtime limits without changing the caller's configuration
or relaxing deployment restrictions.

Fixture DDL, seeding, cleanup, credentials, environments, and CI scheduling belong
to authors or external tools. The CLI does not own those lifecycles. Use a
dedicated read-only account and freeze reproducible fixture writes externally.

## Explicit non-goals and deferred ideas

The following are not required to finish scoped v1:

- `compare_metrics` / `attribute_metric` snapshot grammars or a second analytical
  evaluator. Existing public services and internal workflow tests remain.
- Host-managed Principal injection, policy-denial suites, and permission matrices.
  Test these through host-owned integration tests using existing services.
- Fixture management, containers, migrations, DDL/seed hooks, or a test scheduler.
- Arbitrary JSONPath, expressions, plugins, remote includes, or shell hooks.
- Automatic baselines, expectation approval, candidate-versus-baseline execution,
  affected-case selection, and data-bearing `--include-values` reports.

These are scope decisions, not missing features on a promised implementation
checklist. Revisit an idea only when a concrete user workflow cannot reasonably
be served by the current command or external tests.

## Limits and failure semantics

Shipped limits are 100 cases, a 1 MiB suite, up to 1,000 rows and 1 MiB result
bytes per case, 30 seconds per case, and 10 minutes per suite. Configured query
ceilings can be stricter and are never relaxed. Results are complete or fail;
truncation cannot turn a failure into a pass.

Precision safety covers request inputs as well as result expectations. The tool
must reject numeric filter literals that lose precision through the current
public query decoder; it must not silently change a request to make it executable.
Time expectations beyond the supported nine fractional-second digits fail rather
than truncate. These guards do not redefine public REST/MCP request semantics.

Report outputs cannot alias the suite or configuration. Explicit overwrite only
permits recognizable Metis reports, never arbitrary model/source files. Stable
error codes and safe diagnostic categories must survive redaction so users can
distinguish a timeout, execution limit, or database failure without exposing raw
errors, credentials, endpoints, or result values.

Statuses are `passed`, `failed`, and `not_run`. Exit 0 requires all cases to pass;
1 denotes a failed or incomplete suite; 2 denotes invalid input or report I/O.
Cancellation produces an incomplete/failing run, not a successful skip. Reports
are private and independently atomic; existing output requires explicit overwrite.
See the shipped contract for JSON/JUnit mapping and two-file I/O behavior.

## Acceptance and ownership

Scoped v1 retains these regression obligations:

- Compile assertions detect schema/name/kind/grain and reviewed SQL changes.
- Runtime assertions detect differing business results, preserve large numbers,
  duplicate multiplicity and null/zero distinctions, and reject ambiguous matching.
- Timeouts, execution failures, incomplete results, limits and unexpected denials
  cannot satisfy an expected semantic error or expose sensitive result values.
- CLI composition is explicitly `local_unrestricted`; no host-policy coverage is
  claimed. No suite-supplied physical query or identity is accepted.
- JSON and JUnit agree on outcomes, and required not-run cases fail CI.
- Examples use independently reviewed expectations and separate fixture setup.
  Production tooling works without repository test-package imports.

Internal engine conformance, analytics workflows, and host authorization tests
remain separate; their full scenario corpus need not be reproduced in project
suites. Further maintenance addresses shipped-contract defects, not the removed
expansion checklist. This scope revision remains subject to PR review.

## Documentation and adoption

Authors store reviewed expectations beside models, run them locally and in
existing CI, and review model and expectation changes together. A failing suite
is a reason to investigate, not permission to regenerate expectations. The
[suite guide](../../specs/testing/project-compile-regression.md) includes a
business-regression example and its reviewed fix.

Existing models, REST/MCP interfaces, analytics services, internal tests, and
S2SBench retain their contracts. No migration is required for shipped suites.

## References

- [Project regression suite contract](../../specs/testing/project-compile-regression.md)
- [Semantic source authoring](../../specs/semantic/asset-authoring-lifecycle.md)
- [Public contracts](../../specs/public-contract.md)
- [Testing ownership](../../specs/testing/architecture.md)
- [Runtime fixture example](../../../examples/regression/README.md)
