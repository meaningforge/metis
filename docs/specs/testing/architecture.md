# Testing Architecture Contract

Metis treats tests as part of the public extension contract for community contributors. The test layout is organized by responsibility, not by historical implementation detail.

## Directory contract

Project-owned business regression suites are a separate developer-tooling layer.
`metis project test` answers whether an authored model matches the project's
reviewed expectations; it does not establish Metis engine conformance. Its thin
CLI calls `app/tooling/regression`, which imports existing services but never
repository test or benchmark packages. Compiler and execution services do not
depend on suite parsing or assertions.

Fixture provisioning and CI scheduling remain external. Custom authorization and
tenant-policy matrices belong to host/application integration tests. The current
project command supports compile and metric-result checks only; adding analytics
snapshot grammars or host-policy testing is not required by scoped RFC-0090.
See the [project suite contract](project-compile-regression.md).

```text
tests/
├── conformance/
│   ├── scenarios/        # canonical engine-neutral semantic/query behaviors
│   ├── fixtures/         # canonical semantic-model fixtures
│   ├── compiler/         # common compiler contract + dialect rendering contracts
│   └── upstream/         # compatibility with external semantic specifications/projects
├── engine/
│   ├── datasource/       # test-only DataSource definitions and environment resolution
│   ├── fixture/          # canonical logical tables and rows shared by real engines
│   ├── harness/          # reusable real-engine execution contract
│   ├── clickhouse/       # ClickHouse execution and physical fixture adapter
│   ├── doris/            # Doris execution and physical fixture adapter
│   └── <engine>/         # execution tests
│       └── fixture/      # database setup/type mapping, never copied logical rows
├── samples/              # executable repository examples
├── e2e/                  # deployable API/MCP smoke tests
└── benchmarks/           # benchmark reports and conformance matrices
```

## Hard rules

Data-policy coverage combines application snapshot/failure tests, whole-corpus
dependency and constrained-scan preservation checks, and one shared real-engine
harness contract. The engine contract checks both filtered aggregate results
and preserved rows across a filtered full outer join on all three built-ins.
Policy REST/MCP parity lives in `tests/conformance/agentquery`.

1. **A semantic behavior is defined once.** `tests/conformance/scenarios` is the canonical semantic/query corpus. Do not create ClickHouse-, Doris-, Snowflake-, or other backend-specific copies of an existing semantic scenario.
2. **Fixtures and scenarios are separate.** `tests/conformance/fixtures` identifies reusable semantic worlds; `tests/engine/fixture` defines their canonical logical tables and rows; scenarios define what semantic behavior is being exercised.
3. **Common compiler conformance consumes shared scenarios.** Every formally registered target dialect declares a semantic `CapabilitySet`; the shared compiler contract runs the canonical scenario corpus against that registry. Missing required capabilities fail explicitly.
4. **Dialect Rendering Contracts contain only dialect-specific compiler behavior.** Files such as `tests/conformance/compiler/clickhouse_test.go` may test `sumIf`, ClickHouse time lowering, quoting, casts, or other target-specific SQL behavior, but must not duplicate shared metric/join/filter scenarios.
5. **Real-Engine Execution Contracts use the production execution path.** `tests/engine/<engine>` consumes shared scenarios and owns only physical schema/data setup and connection availability. Every generated semantic query executes as its complete `CompiledQuery` through the production `DataSource -> Backend -> execution/runner -> Driver` path; the engine adapter MUST NOT provide a parallel query executor or result normalizer.
6. **Formal engine support requires both rendering and real-engine execution gates.** Generating plausible SQL alone is not sufficient to claim engine support.
7. **Unsupported behavior must be explicit.** Unsupported semantic or dialect capabilities must return a typed error; silent fallback, guessing, and hidden skips are not acceptable.
8. **Upstream compatibility is separate from Metis semantic behavior.** `tests/conformance/upstream/ossie` validates pinned Apache Ossie schema/examples/converter interoperability; it does not define the Metis semantic scenario taxonomy.
9. **CI orchestrates tests; it does not implement them.** GitHub Actions must call stable Make targets instead of embedding testcase-specific `grep`, SQL, or assertions in workflow YAML.
10. **Benchmarks live with tests.** Benchmark reports and conformance matrices belong under `tests/benchmarks/`; executable correctness, compatibility, conformance, and execution tests remain in their dedicated `tests/` subdirectories.
11. **Internal evidence depth is generated from the registry.** `make semantic-correctness-coverage` generates evidence coverage from canonical scenario metadata. Hand-written benchmark mappings may compare external systems, but they must not redefine whether an internal scenario has compiler- or result-level evidence.
12. **Scenario source files describe semantics, not delivery phases.** Organize canonical scenarios by stable behavior domains or cohesive capabilities such as metrics, joins, time, composition, and semi-additive evaluation. Do not preserve milestone names such as `p0`, `p0b`, or `p1` in the permanent corpus layout.
13. **The required reference reality baseline closes over registered real-engine capabilities.** Every semantic capability declared by a real-engine target must be exercised by at least one required reference case. The baseline also covers every canonical scenario category. This is a minimum regression-pressure contract, not a replacement for executing the full result corpus.
14. **High-risk reality fixtures must be discriminating.** A fixture intended to harden existing semantics must make plausible defects observable with canonical expected results—for example through SQL NULL, zero and negative measures, unmatched relationships, repeated grouping values, deterministic ties, or inclusive time boundaries. Backend setup must reproduce the same physical data world; it must not weaken or reinterpret those signals per engine.
15. **Package dependency guards enforce ownership.** `renderer/sql` may import `query` and `sqlplan` for shared SQL rendering, but no semantic planner, Renderer registry, compiler, or execution package; `compiler/artifact` imports only its closed value dependencies; `renderer`, `planner/conversion`, and `execution/driver` do not import root `compiler`. Root `compiler` alone owns generic compilation orchestration, and `compiler/pipeline` must not reappear.
16. **Default CI executes the self-contained embedded DuckDB Backend gate.** Its complete shared real-engine corpus executes through the production Runner and Driver in the explicit `CGO_ENABLED=1 -tags duckdb` flavor; there is no CLI/subprocess DuckDB query path. ClickHouse and Doris execution conformance remain registered and supported, but run only through an explicit `real-engine-matrix` workflow dispatch whose `engines` input names them (or deliberately selects `all`). The manual matrix defaults to `duckdb` and has no automatic schedule.
17. **Physical fixture data is canonical and shared.** A semantic world defines its logical columns, keys, nullability, and rows once in `tests/engine/fixture`. Each engine adapter may map logical types, quoting, table engines, keys, distribution, and other required DDL options, but must not carry a copied row inventory. A genuinely engine-specific testcase may own a focused local fixture only when the tested behavior itself is physical and cannot be represented by the shared logical contract.
18. **Closed analytics workflows use one typed contract.** Executable production Backends run the shared `attribute_metric` and `compare_metrics` requests and result assertions from `tests/engine/harness`. An engine-specific test may provide runtime assembly and connection plumbing, but must not copy those workflow requests or expected analytics results. A renderer without an executable Backend is not required to claim this runtime contract.
19. **Real-engine connection inputs use checked-in test DataSources.** `tests/engine/datasource/datasources.yaml` follows the Metis named `DataSource -> type + config` shape and is the single test-owned mapping to environment-variable names. Engine test packages resolve a named DataSource instead of reading connection environment variables directly. The file contains no credentials and is not the production `DataSourceRegistry`, Backend registry, or semantic placement authority.
20. **Backend resilience behavior is shared.** `tests/engine/harness` owns the target-neutral failure assertions. Each production backend supplies only its production composition and an engine-local long-running statement. Fault injection stays in the owning Driver package; no test-only hook may enter `execution/driver`.

## Scenario contract

A shared scenario is engine-neutral and carries semantic metadata:

```go
type Scenario struct {
    Name           string
    Category       Category
    Requires       []Capability
    Fixture        fixtures.ID
    Query          query.SemanticQuery
    Verification   Verification
    ExpectedQuery  CompilerExpectation
    ExpectedResult *ResultExpectation
}
```

`Category` and `Requires` describe semantic intent and capabilities such as relationship joins, filters, time grains, derived metrics, ratios, or multi-source composition. They must never encode a SQL dialect or engine name. `Fixture` names the canonical semantic world, including its project/model namespace, independently of a physical file path. `Verification` distinguishes an enforced compiler contract from required or explicitly pending result evidence.

`ExpectedQuery` is the common, target-neutral query-shape obligation consumed by
the compiler contract; dialect-specific syntax remains in renderer tests.
`ExpectedResult` belongs to the scenario, not an engine backend. A required
result contract must have an expectation and no pending reason. A pending result
contract must have no expectation and must explain the gap. This makes every
missing result-level proof visible without treating it as an unsupported
compiler capability.

Compiler targets and real-engine backends both declare a semantic `CapabilitySet`, but for different purposes: compiler targets declare which shared semantic behaviors they can render; real-engine backends declare which shared semantic behaviors they prove by execution. Missing required capabilities are failures for a formally supported shared scenario and are never silently skipped.

## Compiler contract

Compiler tests have two layers:

- **Common Compiler Contract:** `compilerTargets` is the canonical target registry. Each target declares `Name`, `Dialect`, and semantic `CapabilitySet`. The shared scenario corpus is compiled against every registered target.
- **Dialect Rendering Contract:** files such as `clickhouse_test.go`, `doris_test.go`, or future `snowflake_test.go` verify physical SQL behavior unique to that dialect.

A Dialect Rendering Contract answers: **does Metis generate the intended SQL for this target?** Examples include ClickHouse `sumIf` selection and `toStartOfMonth` lowering, Snowflake `VARIANT` access or `QUALIFY`, and Doris-specific function lowering.

A dialect-specific test may introduce a testcase only when the behavior itself is dialect-specific. It must not redefine an existing shared semantic scenario.

Adding a formal compiler target therefore requires:

1. registering the target and its semantic capabilities;
2. passing the shared compiler contract automatically;
3. adding `<dialect>_test.go` only for dialect-specific lowering or syntax.

The target registry, not an ad-hoc list inside individual tests, is the source of truth for which dialects participate in common compiler conformance.

## Real-engine execution contract

All real-engine backends use `tests/engine/harness`. A backend provides only:

- a backend name and declared semantic `CapabilitySet`;
- physical test tables and deterministic data through `Prepare`;
- one production `DataSource`, Backend, and SecretResolver composition through
  `OpenExecution` after each fixture is prepared.

`OpenExecution` constructs a production `execution/runner.Runner`, resolves the
DataSource exactly once, compiles each scenario with the exact Renderer instance
from that resolved Backend, and passes the unchanged route and complete
`CompiledQuery` to `ExecuteResolved`. A fixture-scoped Runner is closed before
the next fixture mutates shared physical table names. Test code MUST NOT perform
a same-name Renderer lookup, execute generated semantic SQL through a raw
database connection, or substitute an inferred/test-only OutputSchema.

Connection plumbing resolves one entry from
`tests/engine/datasource/datasources.yaml`. Like a deployment DataSource file,
it is keyed by stable DataSource name and each record contains `type` and
type-owned `config`. Config is deliberately a flat `map[string]string`; an
exact `${ENV_NAME}` string resolves through a separate connection-value or
secret accessor while the Config entry retains its token; other strings remain
literals. Today ClickHouse owns `scheme`, `address`, `username`,
`password`, and `database`; Doris owns `host`, `port`, `username`, and
`database`. The obsolete combined Doris `address` field is not accepted.
Sensitive reference tokens stay in Config and their resolved values are exposed
only through the test DataSource's narrow secret accessor. A future warehouse
adds only the flat fields required by its implemented Driver contract; the test
framework does not pre-design future authentication variants. Missing inputs
are reported by environment-variable name without exposing resolved values.

This checked-in registry is test infrastructure only. Runtime execution continues to use
the separately referenced deployment `datasources.yaml`, concrete
`DataSourceRegistry`, and `DataSource.type -> Backend` authority. Test
DataSource selection must never become a runtime dialect, Renderer, Driver, or
target override.

Physical setup consumes a canonical logical dataset from `tests/engine/fixture`.
The backend adapter translates its logical types and table metadata into native
DDL. Inserts are generated once from the shared rows. Metis reuses one
semantic/data world and overrides only unavoidable warehouse representation.

The Real-Engine Execution Contract answers: **does the generated SQL execute on the real engine and produce the correct semantic result?** It deliberately does not assert that the SQL text uses one particular equivalent syntax; that belongs to the Dialect Rendering Contract.

The Backend Resilience Contract is a second, orthogonal real-engine contract.
It proves cancellation before execution, bounded engine timeout, row and byte
limits with no partial result, redaction of an oversized row, healthy reuse
after each fault, and rejection after close. Runner-level deterministic fault
tests additionally cover acquisition timeout, execution timeout, row-read
cancellation, decode and partial-stream failures, cleanup failure, secret and
parameter redaction, FIFO admission, queue cancellation, permit accounting,
and shutdown races. Doris and ClickHouse exercise real network transports;
DuckDB exercises the embedded lifecycle. Renderer conformance remains separate.

The shared scenario registry and harness own:

- iteration over the canonical executable scenario corpus;
- grouping scenarios by fixture and preparing each fixture once per backend run;
- capability enforcement;
- expectation completeness checks;
- conversion of the already Runner-normalized `ResultSet` into the coarser
  conformance value families and engine-neutral result comparison.
- execution of registered semantic-critical extension contracts, including
  metric-scale, through the same fixture-scoped production Runner route.

Backends must not carry a map of expected semantic results. They consume the
canonical expectation attached to each result-required scenario.

`Prepare` receives the scenario's stable `fixtures.ID`, not a query name. The
harness creates one fixture-scoped subtest, initializes that physical world
once, and then executes all scenarios registered against it. This permits a
canonical corpus to contain multiple small semantic worlds without forcing one
ever-growing model fixture or allowing physical setup to redefine semantic
queries and expected results.

Multiple fixture IDs may intentionally reuse one embedded Ossie document when
the semantic contract is identical but the physical datasets serve
different correctness purposes. This keeps semantic assets canonical while
allowing a compact happy-path world and a high-entropy adversarial world to
coexist. Their fixture IDs, physical rows, and result oracles remain explicit.

Execution expectations are typed result sets, not backend-formatted TSV/CSV
strings. Each expectation declares logical column names, engine-neutral value
kinds, normalized values, and whether row order is semantically required.
The production Driver reads physical metadata and values; production Runner
then enforces `OutputSchema`, exact Decimal handling, limits, atomicity, and
normalization. Only after that boundary may the harness map Decimal and Float
into the coarser conformance `number` family. Number comparison uses an absolute
tolerance of `1e-9` so equivalent repeating decimal operations remain portable
across engines with different aggregate/division scale rules; Runner output is
not rounded or rewritten. Integer, string, boolean, date, time, datetime, and
opaque values remain distinct. SQL NULL is represented
independently from its physical wire encoding and retains the logical kind of
its column.

Raw engine connections remain permitted only for fixture DDL/DML, readiness
checks, Driver-level contract tests, and S2SBench's deliberately arbitrary SQL
surface. Optimizer differential tests execute both complete compiler artifacts
through production Runner. Focused attribution evidence projections use the
test-only `ProductionExecution.RunRawQuery` boundary with an explicit
compiler-derived `OutputSchema`; Runner and the production Driver still own
execution and normalization. The raw-query helper is not a product API and
MUST NOT be cited as production Backend semantic execution evidence.

Queries with an explicit `order_by` use ordered comparison and MUST preserve
the returned row sequence. Other scenarios use unordered multiset comparison:
presentation order is ignored, but duplicate rows remain significant. Column
order, column names, value kinds, row width, NULL state, and normalized values
are always enforced, subject only to that bounded number tolerance.
Unordered tolerance matching uses a complete bipartite match rather than a
greedy first match, so nearby numeric rows and duplicates cannot make the
verdict depend on engine presentation order.

Local container launchers use process-unique resource names, Docker-assigned
networks, and dynamically published host ports. They MUST NOT reserve a fixed
host port, container name, or private subnet. Engine-specific scripts own only
image configuration and readiness evidence; shared collision-free lifecycle
plumbing lives under `tests/engine/container/`.

A backend must not maintain its own semantic query or expectation list. Adding a
result-required scenario therefore automatically creates an obligation for
every formally supported execution backend: declare and execute the required
capability, or fail explicitly.

A future execution engine therefore adds a Renderer/Backend/DataSource through
the production registries, one physical fixture adapter, one checked-in test
DataSource record, capability registration, and—only when locally
containerizable—an engine readiness launcher. It does not add an executor,
result parser, semantic corpus, comparison algorithm, or Runner lifecycle.
Registered extension cases are automatically inherited; an engine adapter
only maps their canonical logical fixture types to physical DDL. Optimizer and
focused raw-projection contracts inherit the same Runner-backed test utility.

The same ownership rule applies above individual SQL scenarios. The public
`attribute_metric` and `compare_metrics` execution workflows are defined by one
typed harness contract using a shared Ossie model and logical dataset. A
production Backend proves the workflow by wiring its service methods into that
contract. Compile-only Renderer support remains a separate claim and must not
be represented as successful runtime workflow coverage.

## Contract stack

```text
Shared Semantic Scenario
        │
        ▼
Common Compiler Contract
        │
        ├── Dialect Rendering Contract
        │       "Is target SQL rendered correctly?"
        │
        ▼
Generated SQL
        │
        ▼
Real-Engine Execution Contract
        │       "Does it execute and return the right semantics?"
        ▼
Normalized Expected Result
```

The two target-specific contracts are intentionally independent. A renderer can regress from an intended ClickHouse-specific form to a semantically equivalent generic form and still pass execution; the Dialect Rendering Contract catches that. Conversely, SQL can look correct while failing or returning incorrect results on a real engine; the Real-Engine Execution Contract catches that.

## Standard entry points

```bash
make check                  # required correctness gate, no live database needed
make test-e2e               # deployable API/MCP smoke test
make test-engine-clickhouse # real ClickHouse execution conformance
make test-engine-doris      # real Doris execution conformance
make semantic-correctness-coverage       # regenerate corpus evidence report
make semantic-correctness-coverage-check # reject stale corpus evidence report
```

Real-engine backends follow the naming convention `make test-engine-<engine>`.

The independent-host integration test inherits `GOPROXY` and `GOSUMDB` from
the environment. A fresh module cache requires dependency downloads, including
transitive module metadata that a repository-only build may not fetch. Offline
runs require the independent host's dependencies to be cached as well.

## Adding a new dialect or engine

A contributor adding a new target should normally:

1. implement the renderer/compiler support;
2. register the compiler target and its semantic capabilities;
3. make the target pass the Common Compiler Contract;
4. add `<dialect>_test.go` for the Dialect Rendering Contract only when target-specific SQL behavior exists;
5. declare the real-engine backend semantic capabilities;
6. add a backend using `tests/engine/harness` to satisfy the Real-Engine Execution Contract when formal engine support is claimed;
7. implement a physical fixture adapter over `tests/engine/fixture` for DDL/DML only, but no copied logical rows, semantic query, query executor, result normalizer, or expectation inventory;
8. add its named `type + config` entry to `tests/engine/datasource/datasources.yaml`, referencing environment variables rather than storing endpoints or credentials in tests;
9. add no testcase-specific logic to GitHub Actions.

The intended invariant is:

> Semantic scenarios define behavior once; compiler tests prove rendering contracts; real-engine tests prove execution semantics.
