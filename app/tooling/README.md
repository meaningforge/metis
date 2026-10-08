# Project-author tooling

This directory contains reusable application workflows shipped through the
`metis` CLI to help authors create and check semantic projects.

| Package | Responsibility | CLI entry point |
| --- | --- | --- |
| [authoring](authoring/) | Normalize bounded catalog evidence, validate explicit mappings, generate a reviewable Ossie candidate, and publish local files safely. | `metis catalog inspect`, `metis semantic init` |
| [regression](regression/) | Validate project-owned suites, invoke compile/query services, compare reviewed expectations, and produce JSON/JUnit reports. | `metis semantic test` |

## Dependencies and ownership

The CLI owns argument parsing, process cancellation, user-facing output and exit
codes. Tooling packages own input contracts and workflow orchestration. Existing
application services and bootstrap own source loading, semantic validation,
authorization, compilation and runtime assembly.

The dependency direction is:

```text
cmd/metis -> app/tooling -> app/service and app/bootstrap
```

Tooling may use existing model, artifact, renderer and execution APIs as required
by a workflow. Compiler, planner, renderer, execution and application services
must not depend on tooling schemas or assertions. Production tooling must not
import CLI packages, `tests/**`, or benchmark-owned packages. Each package should
remain usable by a trusted application caller without importing the executable.

The directory belongs under `app` because these workflows compose application
capabilities. Repository [tests](../../tests/) verify Metis itself, while
[tools](../../tools/) primarily support repository maintenance and CI. A shipped
project-testing workflow has different ownership from those internal facilities.

## Contracts

`metis semantic` owns authoring and project-owned compile/result checks. It must
not add agent runners, frozen benchmark suites, scoring or paired experiments:
those belong to `s2sbench`. Conversely, S2SBench consumes the semantic runtime;
it is not a second authoring CLI. Sharing application services is allowed;
duplicating these command workflows or depending on benchmark implementations
from Metis tooling is not.

Keep business decisions explicit: generation produces a review candidate;
regression compares independently reviewed expectations. Fixture provisioning,
CI scheduling and managed publication lifecycles stay with their external owners.
Reuse the existing query and validation paths rather than adding another semantic
evaluator or SQL executor here.

Current schemas, supported operations, limits and compatibility behavior are
defined in the specifications:

- [Offline catalog-assisted authoring](../../docs/specs/semantic/catalog-authoring.md)
- [Project regression suites](../../docs/specs/testing/project-compile-regression.md)
- [Testing architecture](../../docs/specs/testing/architecture.md)
