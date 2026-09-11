# RFC-0061: Planner Package Responsibilities and IR Conversion Layout

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-30
- **Last updated:** 2026-08-31
- **Scope:** `planner/`, `sqlplan/` boundary, planner-facing compiler integration, package dependency rules, planner tests and documentation
- **Supersedes:** None
- **Related:** RFC-0037, RFC-0038, RFC-0039, RFC-0040, RFC-0052, RFC-0058, RFC-0060, ADR-0005, ADR-0010

## Summary

This RFC reorganizes the broad `planner` Go package into responsibility-led
subpackages without changing Metis semantic meaning, renderer authority,
physical SQL output, or execution behavior.

The resulting layout makes the three planner IR stages directly visible:

```text
SemanticQuerySpec
    -> MetricEvaluationPlan
    -> SemanticPlan
    -> SQLPlan
```

The proposed package layout is:

```text
planner/
    planner.go

    evaluation/
    semanticplan/
    builder/
    optimizer/
    attribution/
    conversion/
    temporal/                   shared built-in time-grain value operations

sqlplan/
renderer/
compiler/
compiler/attribution/
```

`planner/conversion` is the current `SemanticPlan -> sqlplan.Plan` producer.
It is deliberately named for planner-IR conversion, rather than for SQL
rendering or the `conversion` metric kind. `sqlplan` and `renderer` remain
top-level, independent physical boundaries.

This layout names the major domains known today; it is not a closed list of
the only permitted planner subpackages. A newly identified domain may receive
a focused package when it has a concrete responsibility, a one-way dependency
boundary, and a name that makes its role clear without reading its files.

## Motivation

`planner/` currently contains metric evaluation, the `SemanticPlan` IR and
its node implementations, source-aware plan construction, optimizer rules,
attribution planning, and every semantic-aware SQLPlan rewrite. The package
therefore combines several independently evolving domains behind one import
path and a large flat file list.

The existing compilation architecture already defines distinct authorities:

- `MetricEvaluationPlan` owns metric roots, kinds, dependency closure, and
  metric-local evidence.
- `SemanticPlan` owns the source-aware logical DAG.
- `Optimizer` performs semantics-preserving rewrites on that DAG.
- `sqlplan.Plan` is the independent, renderer-neutral physical SQL IR.
- `Renderer` is the sole dialect-specific rendering extension point.

The source tree should make those authorities as easy to discover as the
architecture makes them to reason about. The reorganization also provides a
clear home for future planner-IR conversions without making `sqlplan` a child
of semantic planning.

This RFC follows the useful separation of metric evaluation, dataflow-plan
construction, optimization, and plan conversion found in MetricFlow, but keeps
Metis's established vocabulary: `SemanticPlan` remains Metis's canonical
source-aware logical IR. It does not introduce `DataflowPlan` as a second
Metis IR or public term.

## Goals and non-goals

### Goals

- Make planner package paths reflect one concrete responsibility and its
  appropriate ownership boundary.
- Preserve the canonical flow from `MetricEvaluationPlan` through
  `SemanticPlan` to `sqlplan.Plan`.
- Make the distinction between conversion metrics and plan-to-plan conversion
  obvious from package and file names.
- Preserve `sqlplan` as the typed physical IR independent from planner and
  renderer implementation.
- Preserve the one selected Renderer identity from resolution through final
  rendering.
- Complete a clean-cut package migration in which repository callers import
  the package that owns each contract directly.
- Preserve conformance SQL bytes and parameters except for separately reviewed
  correctness fixes.

### Non-goals

This RFC does not:

- change semantic query behavior, metric meaning, optimizer behavior, or
  output-schema semantics;
- add an execution plan, runtime placement, a second semantic IR, or a
  general non-SQL physical IR;
- move `sqlplan` under `planner`;
- move dialect selection, renderer registry lookup, or `Renderer.Render` into
  `planner`;
- make planner subpackages a new third-party extension SPI; or
- rename the public domain concepts `MetricEvaluationPlan`, `SemanticPlan`,
  `SQLPlan`, or `Renderer`.

## Design

### Package layout and responsibilities

```text
planner/
    planner.go                 top-level construction/optimization orchestration

    evaluation/                MetricEvaluationPlan domain IR
    semanticplan/              SemanticPlan domain IR
    builder/                   resolved request + evaluation -> SemanticPlan
    optimizer/                 SemanticPlan -> optimized SemanticPlan
    attribution/               metric-change attribution planning
    conversion/                planner IR -> planner/physical IR conversion

sqlplan/                       renderer-neutral typed physical SQL IR
renderer/                      SQL dialect renderer contract, registry, and implementations
compiler/                      selected-Renderer physical compilation orchestration
compiler/attribution/          attribution-specific compiled bundle orchestration
```

The role of each planner package is normative.

| Package | Owns | Must not own |
| --- | --- | --- |
| `planner` | `Planner`, `New`, `Plan`, runtime observation, construction of default collaborators, and top-level orchestration | re-exported domain types, conversion/optimizer forwarders, renderer lookup, SQL text rendering |
| `planner/evaluation` | `MetricEvaluationPlan`, typed metric specs, metric roots/roles, dependency closure, validation, clone, explanation, and fingerprinting | source roots, joins, SQL blocks, output grain, or physical strategy |
| `planner/semanticplan` | `SemanticPlan`, its concrete typed nodes, logical output contract, DAG ownership, validation, clone, explanation, and fingerprinting | raw semantic resolution, metric dependency discovery, `artifact.OutputSchema`, or SQL rendering |
| `planner/builder` | source-aware construction from a resolved request and a validated evaluation plan, including source selection, relationships, grain, time/calendar behavior, and metric-semantic node construction | renderer lookup, SQLPlan construction, SQL text rendering, re-derivation of metric evaluation closure/kind |
| `planner/optimizer` | deterministic, proof-gated rewrites and requirement analysis over an owned `SemanticPlan` | semantic identity resolution, metric-definition selection, raw model interpretation, SQLPlan construction |
| `planner/attribution` | attribution request, plan, bundle, validation, and construction of its independent SemanticPlans | closed `SemanticPlanNode` implementations, generic Renderer selection, generic physical compilation, execution |
| `planner/conversion` | planner IR conversion; initially `SemanticPlan -> sqlplan.Plan`, semantic-aware typed SQLPlan rewrites, and `SemanticPlan -> artifact.OutputSchema` derivation | SQL dialect syntax, SQL string rendering, renderer registry lookup, execution |
| `planner/temporal` | built-in time-grain ordering and timezone-preserving temporal literal operations shared by construction and conversion | planner state, metric semantics, predicate placement, custom-calendar resolution |
| `sqlplan` | typed, renderer-neutral physical relations, expressions, query blocks, validation, cloning, explanation, and fingerprints | `SemanticPlan`, metric meaning, Renderer, target authority |

`planner/conversion` is intentionally a planner-level package. Its current
conversion is `SemanticPlan -> sqlplan.Plan`, but a future distinct
planner-IR conversion may be added only when it has a named input/output
contract and does not duplicate semantic or physical authority. A future
conversion does not authorize an execution plan or a second physical IR.

`planner/conversion` owns `BuildOutputSchema`. The
function derives the target-neutral `artifact.OutputSchema` from the validated
semantic output contract; it never parses rendered SQL or infers database wire
types. `lowering_strategy.go`, `output_schema.go`, and every semantic-aware SQLPlan
post-processing step move to this package. `runtime_observer.go` remains with
the root `planner` package because it observes planning and optimization
lifecycle orchestration rather than an IR.

### Package design principles

Package extraction is guided by domain cohesion, not by a fixed number of
packages or files. A package must have a nameable, specific responsibility
that can be stated in terms of what it owns, its inputs and outputs, and the
dependencies it must not take. The directory name should communicate that
responsibility directly; broad names such as `utils`, `helpers`, `common`, or
`advanced` do not establish an architectural boundary.

The six public planner domains above are the current primary IR and workflow
boundaries, not an exhaustive taxonomy. Additional focused domains are
permitted when they reduce a coherent cluster of related files and tests,
avoid mutual imports with adjacent domains, and do not introduce a competing
semantic authority. Conversely, a package must not be created solely because
one file contains a reusable function.

Planner package extraction is limited to one directory level below `planner/`.
Do not create nested paths such as `planner/optimizer/source_scan` or
`planner/internal/<domain>` merely to mirror conceptual layers. A focused
package is justified only when multiple responsibility owners need the same
cohesive contract. `planner/temporal` meets that bar because builder and
conversion both require the same built-in grain ordering and temporal-literal
operations. It owns neither predicate placement nor metric semantics.

Relationship cardinality, fanout admission, and population-preservation proof
remain in `planner/builder`, where relationship traversal is constructed.
Source-scan fusion and population-proof filtering remain private optimizer
rules in `planner/optimizer`. A single helper file does not justify a separate
`planner/join` or `planner/source_scan` package and a wider exported API.

### MetricFlow reference model

MetricFlow is a reference for naming and ownership, not a target for a
line-for-line port. Its planner implementation is not a small collection of
Python files: it separates a metric-evaluation plan, dataflow-plan construction,
dataflow optimization, and dataflow-to-SQL conversion into nested paths. Within
those paths, module names state a specific role, such as
`dataflow_plan_builder`, `source_node_recipe`, `source_scan_optimizer`,
`dataflow_to_cte`, `dataflow_to_subquery`, and `sql_join_builder`.

Metis should apply the same discoverability standard while preserving its own
SemanticPlan and SQLPlan authority boundaries. A future package or file must
be named for its concrete input/output transformation or evidence domain, not
for an implementation convenience. Source-scan optimization belongs with the
other rules in `planner/optimizer`; physical-plan conversion belongs in
`planner/conversion`; and relationship-safety proof belongs with source-aware
construction in `planner/builder`. This preserves clear ownership without
duplicating MetricFlow's nested package topology or widening helper APIs.

### Naming rules

`conversion` has two different, deliberately disambiguated uses:

```text
planner/conversion/                              plan-to-plan conversion
planner/semanticplan/node_conversion_metric.go   conversion metric node
planner/builder/metric_conversion.go             conversion metric construction
```

No file or package representing the conversion metric belongs in
`planner/conversion`. No file generating `sqlplan.Plan` belongs in the metric
conversion domain.

### Attribution node ownership

`SemanticPlanNode` is a closed interface with a package-private marker.
`AdditiveAttributionNode` and `RatioAttributionNode` therefore belong in
`planner/semanticplan`, alongside every other concrete node. That package also
owns their marker methods, node-local payload validation, clone support, and
the exhaustive validation and explanation switches.

`planner/attribution` owns the attribution request, attribution plan, bundle,
proof validation, and construction workflow. After it has proven attribution
semantics, it constructs the exported typed nodes supplied by
`planner/semanticplan`; it does not implement a node or marker itself. Any
payload type stored in a closed attribution node, such as period evidence, must
be owned by `semanticplan` or a package below it in the import graph. It must
not require `semanticplan` to import `attribution`.

Attribution physical lowering belongs in `planner/conversion`; compiled-bundle
orchestration remains in `compiler/attribution`. This produces the complete
ownership sequence:

```text
attribution request/plan/bundle  -> planner/attribution
closed attribution node          -> planner/semanticplan
attribution SQLPlan lowering     -> planner/conversion
compiled attribution bundle      -> compiler/attribution
```

The repository must not introduce a catch-all `advanced/` package. Cumulative,
time-offset, offset-to-grain, custom-calendar, dense-calendar, semi-additive,
and conversion metric behavior are typed metric semantics. Their node types
belong in `semanticplan`; their construction belongs in `builder`.

The repository must not use a generic `lowering/` package for this layout.
There are two different lowerings in the pipeline, and the word alone obscures
their boundaries:

```text
planner/builder       MetricEvaluationPlan -> SemanticPlan
planner/conversion    SemanticPlan -> sqlplan.Plan
renderer              sqlplan.Plan -> SQL text + parameters
```

### Dependency direction

The package graph must remain acyclic and authority-directed:

```text
planner ------------> evaluation + builder + optimizer + semanticplan
builder ------------> evaluation + semanticplan + temporal
optimizer ----------> semanticplan
attribution --------> evaluation + semanticplan
conversion ---------> semanticplan + temporal + sqlplan + compiler/artifact

compiler -----> planner/conversion + selected renderer -----> renderer.Render(sqlplan.Plan)
compiler/attribution --> planner/attribution + compiler
```

The diagram describes authority, not permission to create import cycles. In
particular:

- `semanticplan` must not import `builder`, `optimizer`, `attribution`, or
  `conversion`. Its package-private `SemanticPlanNode` marker, concrete-node
  switches, cloning, validation, and explanation remain in this one package.
- `builder` may consume a selected Renderer's already-supplied expression
  dialect/capability evidence, but must not look up a Renderer.
- `optimizer` must depend only on typed semantic-plan evidence and must not
  import `sqlplan` or `renderer`.
- `conversion` may consume a validated `SemanticPlan`, `sqlplan`, and the
  already-selected Renderer evidence necessary to validate opaque expression
  compatibility. It must not call `Renderer.Render` or resolve a Renderer.
- `sqlplan` must not import any `planner` package. `renderer` must consume
  `sqlplan.Plan` without recovering `SemanticPlan` authority.
- `compiler` remains the generic one-plan physical-compilation
  orchestrator: it performs at most one registry lookup, calls the planner
  conversion entry point with that exact Renderer, then calls that same
  Renderer to render the resulting `sqlplan.Plan`.

The following table is the required Go import graph. Direct imports of external
domain packages such as `manifest`, `resolver`, `query`, and `renderer` remain
allowed only where listed responsibility requires them.

| Package | May import planner packages | Must not import planner packages |
| --- | --- | --- |
| `planner` | `evaluation`, `builder`, `optimizer`, `semanticplan` | `attribution`, `conversion`, `temporal` |
| `planner/evaluation` | none | every other planner package |
| `planner/semanticplan` | none | every other planner package |
| `planner/builder` | `evaluation`, `semanticplan`, `temporal` | `planner`, `optimizer`, `attribution`, `conversion` |
| `planner/optimizer` | `semanticplan` | `planner`, `evaluation`, `builder`, `attribution`, `conversion`, `temporal` |
| `planner/attribution` | `evaluation`, `semanticplan` | `planner`, `builder`, `optimizer`, `conversion`, `temporal` |
| `planner/conversion` | `semanticplan`, `temporal` | `planner`, `evaluation`, `builder`, `optimizer`, `attribution` |
| `planner/temporal` | none | every other planner package |

`planner/builder` may also import `resolver` and other already-resolved
semantic evidence. `planner/attribution` may import `manifest` for attribution
proofs. `planner/conversion` may import `sqlplan`, `compiler/artifact`, and the
`renderer` contract. All planner subpackages must not import the root
`planner` package.

### Renderer evidence contract

The selected `renderer.Renderer` instance, not a copied dialect/capability
configuration, is the evidence value passed across planner boundaries. The
root orchestrator passes its selected instance unchanged to `builder`; physical
compilation passes that same instance unchanged to `conversion` and then to
`Renderer.Render`.

The target function-signature policy is:

```go
builder.Build(ctx, resolved, evaluationPlan, metricEvaluationRequired, selectedRenderer)
conversion.BuildSQLPlan(semanticPlan, selectedRenderer)
selectedRenderer.Render(sqlPlan)
```

The signatures may add narrowly typed non-authority inputs, but they must not
replace `selectedRenderer` with reconstructed dialect/capability structs. No
planner subpackage may construct, normalize, or re-resolve Renderer evidence.

### Clean-cut package migration

This repository is not yet published with a stable planner Go API, so preserving
the former root entry points is not a migration requirement. The final merged
state MUST contain no root type aliases or forwarding functions for evaluation,
semantic-plan, optimizer, attribution, conversion, validation, explanation, or
fingerprint contracts.

All repository callers MUST import the real owner directly. For example,
callers use `*semanticplan.SemanticPlan`, `semanticplan.Validate`,
`semanticplan.Explain`, `semanticplan.Fingerprint`,
`conversion.BuildSQLPlan`, and `conversion.BuildOutputSchema`. The root
`planner` package exposes only `Planner`, `New`, `Plan`, runtime observation,
and the top-level orchestration required to connect builder and optimizer.

Migration work may be split across commits, but aliases and forwarders are
temporary implementation state and MUST NOT exist in the final merge result.

Planner subpackages are repository implementation boundaries, not independent
Agent-facing APIs or warehouse extension points. REST, MCP, and execution
surfaces continue to use application services and compiler contracts, never
planner implementation packages directly.

### Subpackage API exposure

Go makes every non-`internal` package importable, so directory extraction must
not mechanically turn package-private helpers into a broad exported API. Each
subpackage must expose only its named IR types and a small, documented set of
entry points needed by its permitted importers; helper functions remain
unexported.

Before extracting a helper-only dependency, implementation must decide whether
it has a durable domain contract. Helpers with no independent IR or ownership
contract remain co-located with their owner. A helper that does have a focused
cross-domain contract may use one direct, concrete `planner/<domain>` package;
the named packages in this RFC are not a third-party extension SPI, even if
their Go import paths are technically visible.

### Semantic and physical contracts

The move must retain all current contracts:

1. Every metric-bearing request creates one validated
   `MetricEvaluationPlan` before semantic-plan construction; metric-free
   requests create no fabricated metric plan.
2. `builder` receives that evaluation plan and may not rediscover metric roots,
   dependency edges, or metric kind from raw model declarations.
3. Every successful build creates one owned, validated `SemanticPlan` DAG.
4. `optimizer` clones/rewrites according to its existing deterministic and
   proof-gated contract, and invalid state fails before physical conversion.
5. `conversion` has exactly one `SemanticPlan` entry and produces a complete,
   validated, owned `sqlplan.Plan`, plus the target-neutral output schema
   derived from the semantic output contract. It may select an already-defined
   compact, composed, or attribution physical shape, but may not reinterpret
   semantic meaning or add target authority.
6. A selected Renderer instance remains the only source of expression-dialect
   and capability evidence from resolution through rendering. No moved package
   may perform a same-name second registry lookup.
7. `renderer` alone owns target syntax and conversion from `sqlplan.Plan` to
   SQL text and parameters. It does not consume `SemanticPlan`.

### File migration guide

The following is a responsibility guide, not a requirement to preserve every
current filename verbatim:

| Current file family | Destination |
| --- | --- |
| `metric_evaluation_*` | `planner/evaluation/` |
| `semantic_plan_*`, plan ownership/validation/explain/fingerprint and logical output-contract code | `planner/semanticplan/` |
| concrete source, aggregate, composition, calendar, time, and metric node definitions | `planner/semanticplan/` |
| `semantic_construction_*`, `source_selection_*`, shared-grain construction, conversion/time/calendar/semi-additive construction | `planner/builder/` |
| relationship fanout admission and population-preservation proof | `planner/builder/` |
| `optimizer_*`, redundant-work and requirements rules | `planner/optimizer/` |
| source-scan fusion and scan-equivalence grouping | `planner/optimizer/` |
| built-in grain ordering and temporal literal operations shared across stages | `planner/temporal/` |
| attribution request, plan, bundle, proof, and construction workflow | `planner/attribution/` |
| `AdditiveAttributionNode`, `RatioAttributionNode`, their marker/local validation/clone/explain participation | `planner/semanticplan/` |
| `sql_plan_*`, `lowering_strategy.go`, `output_schema.go`, and semantic-aware typed SQLPlan post-processing | `planner/conversion/` |
| `runtime_observer.go` | root `planner/` orchestration package |

Files should use domain-qualified names where a short name could be confused
across packages. In particular, use `node_conversion_metric.go` for the
conversion metric node and `metric_conversion.go` for its builder behavior.
Package-level tests move with the domain they verify; only root Planner
orchestration tests, cross-package integration contracts, and end-to-end tests
remain at the root or conformance boundary. Test-file count is not itself a
reason to merge unrelated scenarios into broad files.

## Alternatives

### Keep one flat `planner` package

Rejected. Go files in one package can share unexported implementation details,
but the current scale hides distinct IR and transformation boundaries. Prefixes
alone do not make ownership, allowed dependencies, or the future home of new
logic clear.

### Use `lowering/` and `advanced/`

Rejected. `lowering` ambiguously describes both semantic construction and
SemanticPlan-to-SQLPlan conversion. `advanced` describes a perceived
difficulty level rather than a stable semantic domain and would become a
catch-all package.

### Put conversion under `semanticplan/`

Rejected. `SemanticPlan -> sqlplan.Plan` exits the logical semantic IR and is
best represented as a planner-level IR conversion boundary. Keeping it at
`planner/conversion` also provides a principled home for future explicit
planner-IR conversion contracts.

### Name `planner/conversion` `plan_conversion`

Rejected. The package is already scoped by `planner`; `conversion` is concise
and the package comment plus the naming rules above distinguish it from a
conversion metric.

### Adopt MetricFlow's `dataflow/` term verbatim

Rejected. MetricFlow's separation of metric evaluation, plan building,
optimization, and plan conversion is useful precedent. Metis already has the
well-specified `SemanticPlan` term, however, and introducing `DataflowPlan`
would create unnecessary competing vocabulary.

### Move `sqlplan` below `planner/`

Rejected. `sqlplan.Plan` is the renderer-neutral physical boundary consumed by
top-level `renderer/`. Nesting it under planner would blur the authority break
between semantic planning and physical rendering, and would make a renderer
appear to depend on semantic planning.

## Rollout and migration

The migration is behavior-preserving and may be reviewed in bounded commits,
but its final state is one clean package cutover:

1. Add package-level `doc.go` files and architecture tests for the target
   dependency direction before moving behavior.
2. Extract `evaluation` and `semanticplan` contracts and migrate all repository
   callers to those owner packages in the same merge.
3. Extract `semanticplan` with every concrete node, including attribution
   nodes, and all IR invariants in one Go package. Extract attribution request,
   proof, bundle, and construction workflow separately without making
   `semanticplan` import `attribution`; then move semantic construction and
   all metric/time/calendar construction to `builder`.
4. Extract the optimizer and preserve its rule order, copy-before-rewrite
   behavior, fixed-point behavior, and differential evidence.
5. Extract attribution without moving generic physical compilation out of
   `compiler/attribution` or `compiler`.
6. Move every `sql_plan_*` producer, lowering strategy, output-schema builder,
   and typed physical post-processing step to `planner/conversion`; migrate all
   callers directly to `conversion.BuildSQLPlan` and
   `conversion.BuildOutputSchema`.
7. Delete every temporary root alias and forwarder before merge and verify that
   repository callers import their responsibility owner directly.

Each step must be independently revertible. No step may combine a package move
with an intentional semantic or rendered-SQL change unless that correctness
change is separately documented and reviewed.

## Test and acceptance criteria

Before this RFC can become `Implemented`:

- `go test ./...`, `go vet ./...`, and `make ossie-conformance` pass.
- Existing optimizer differential, semantic-plan quality, output-schema,
  attribution, renderer-authority, and SQLPlan validation tests pass without
  weakened assertions.
- Existing SQL conformance fingerprints and parameter evidence remain stable
  unless an independently documented correctness fix changes them.
- Architecture tests prove that `sqlplan` imports no planner package, renderer
  production packages import no planner package, and `optimizer` imports
  neither `sqlplan` nor `renderer`.
- Architecture tests prove that `conversion` does not perform renderer registry
  lookup or call `Renderer.Render`, and that `compiler` retains the
  selected-Renderer single-lookup/same-instance flow.
- Architecture tests enforce the Go import table above, including that all
  planner subpackages avoid the root `planner` package and `semanticplan`
  never imports `attribution`.
- Architecture checks prove that root `planner` contains only orchestration and
  runtime observation, with no domain aliases or forwarders.
- Repository callers use direct owner imports for semantic-plan validation,
  explanation, fingerprinting, output-schema construction, SQLPlan conversion,
  optimizer contracts, and attribution.
- Each subpackage exposes only documented entry points. No helper is exported
  solely to make a mechanical move compile, and implementation records why a
  helper merits a dedicated one-level package rather than co-location.
- Package comments document inputs, outputs, ownership, and forbidden
  responsibilities for every new planner subpackage.

## Documentation updates

On implementation, update:

- `docs/design/semantic/pipeline.md` with the package-level planner map and
  the `evaluation -> builder -> semanticplan -> optimizer -> conversion`
  flow;
- `docs/specs/semantic/compilation-pipeline.md` where it names planner-owned
  construction, optimization, and SQLPlan production boundaries;
- `docs/specs/sql/dialect-rendering.md` only as needed to retain the exact
  `planner` conversion and `sqlplan`/`renderer` boundary wording;
- `docs/README.md` code-to-document map if package paths change; and
- package-local `doc.go` files as the non-duplicated operational guide for
  contributors.

No current specification changes merely because this RFC is drafted. Those
updates occur only with the implemented package migration.
