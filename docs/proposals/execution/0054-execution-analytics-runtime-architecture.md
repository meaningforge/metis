# RFC-0054: Execution Runtime and Analytics Runtime Architecture

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-29
- **Last updated:** 2026-09-01
- **Accepted:** 2026-08-29
- **Implemented:** 2026-09-01
- **Scope:** semantic/runtime boundaries, execution orchestration, analytical runtime architecture, deployment modes, future public execution and analytical capabilities
- **Supersedes:** product-wide compile-only boundary statements in RFC-0051 and RFC-0052, plus ADR-0008's public compile-only attribution decision; current contracts retain compile-only deployment as a supported mode
- **Partially superseded by:** [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and ADR-0010 for compile authority, project resolution, DataSource/Backend vocabulary, and runtime routing
- **Related:** RFC-0034, RFC-0036, RFC-0038, RFC-0045, RFC-0051, RFC-0052, RFC-0053, ADR-0008

## 1. Summary

This RFC defines Metis's evolution from a semantic-to-SQL compiler into a
layered semantic runtime for Agentic Analytics without weakening the
deterministic compiler core.

The architecture is implemented by the [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and RFC-0056 through RFC-0064. Doris and
DuckDB satisfy the two-Backend gate; `attribute_metric` and `compare_metrics`
satisfy the two-workflow gate. ADR-0013 records that this evidence does not by
itself authorize a generalized analytical IR.

The implemented architecture is:

```text
                         Metis
                           |
                           v
                 Orchestration Layer
                  /        |        \
                 v         v         v
       Semantic Core  Execution    Analytics
                      Runtime      Runtime
             |           |           |
             |           |           +-- attribution
             |           |           +-- comparison
             |           |           +-- future analytics
             |           |
             |           +-- DriverFactory Registry
             |           +-- DataSource -> Doris instance
             |           +-- DataSource -> DuckDB instance
             |           +-- DataSource -> Snowflake instance
             |           +-- multiple instances of the same driver
             |           +-- multiple driver families in one deployment
             |
             +-- Semantic Graph
             +-- Analyzer
             +-- Planner
             +-- Optimizer
             +-- SQL Compiler
```

Semantic Core itself is fed by an immutable `SemanticManifest`. Future physical
Catalog integrations may contribute validated physical metadata to manifest
construction, but request-time resolution reads one fixed manifest rather than
calling a mutable external metadata service mid-plan.

The architectural commitment is:

> **Semantic Core remains deterministic and database-independent.**
>
> **Execution becomes an optional runtime capability, not a compiler dependency.**
>
> **Analytics Runtime builds deterministic multi-query analytical workflows on
> top of semantic compilation and warehouse execution.**
>
> **A transport-neutral Orchestration Layer dispatches capabilities, resolves
> explicit data-source routes, and coordinates bounded workflows without owning
> semantic planning, driver execution, or analytical mathematics.**

The accepted product direction remains compatible with the existing principle:

> **Agents reason. Metis resolves semantics. Engines execute.**

Here, `engines execute` means the physical warehouse continues to execute SQL.
Metis may orchestrate that execution through an optional runtime and evaluate the
returned data into deterministic analytical results. Metis does not become a
database engine.

The intended public capability vocabulary is task-oriented and explicit:

```text
compile_sql
query_metrics
attribute_metric
compare_metrics
```

`compile_sql` compiles a governed semantic metric query into physical SQL without
executing it. `query_metrics` accepts the same governed semantic query shape,
compiles through the same canonical compiler path, executes the resulting
`SQLRenderResult`, and returns normalized query results. `attribute_metric` owns a
complete metric-change attribution workflow and returns a structured analytical
result. `compare_metrics` owns a complete two-period shared-grain comparison
workflow and returns aligned values, delta, and percent change.

The names are intentionally specific. `compile_sql` makes the compiler-only
boundary visible at the tool name. `query_metrics` avoids implying that Metis
executes arbitrary SQL and states that the operation queries governed metrics.
`attribute_metric` and `compare_metrics` identify their higher-level analytical
responsibilities.

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) refines the accepted execution vocabulary into separate type-level and
instance-level concepts:

```text
DataSource
    one configured database or warehouse instance

Backend
    one database-family implementation selected by DataSource.type
    binds SQLDialect + Renderer + DriverFactory

DriverFactory
    registered Go implementation for one Driver
    validates driver-owned configuration and opens an Executor

Executor
    Metis' small engine-neutral SQLRenderResult execution contract
```

Cube remains useful prior art for named data sources and driver factories, but
Metis does not preserve a model-level ExecutionBinding or route alias. Root
Project Registration references a DataSource directly; `DataSource.type`
selects Backend, and Executor remains the post-compilation runtime boundary.

The architecture was delivered through these follow-on contracts:

```text
Execution Runtime Contracts
          DataSource / Driver / DriverFactory / SecretRef / Executor / ResultSet

RFC-0056  Runtime Metric Query Execution
          Orchestration Layer / query_metrics / transport-service behavior

RFC-0057  Production Metric Attribution Runtime
          attribute_metric / Attribution Evaluator / AttributionResult

RFC-0064  Production Metric Comparison Runtime
          compare_metrics / Comparison Evaluator / CompareMetricsResult
```

A key accepted product-surface decision is:

> **Attribution compilation remains an internal planner/runtime stage, not a
> public Agent, REST, or MCP capability.**

`attribute_metric` is the public analytical capability. It internally plans and
compiles the required attribution query bundle, executes that bundle, and
evaluates the returned evidence.

### 1.1 Status and authority

This RFC is Implemented. [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and RFC-0056 through RFC-0064, ADR-0010 through ADR-0013,
and the linked current specifications now define the production contracts.

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) and ADR-0010 now supersede this RFC's detailed `ExecutionBinding`,
Driver-selection, route-map, and `CompileTarget` design. The durable authority
retained here is the four-layer architecture, optional execution, orchestration
ownership, the `SQLRenderResult` handoff, and the task-oriented public capability
direction.

Optional Execution Runtime now owns bounded physical execution; compile-only
deployment remains supported. `compile_sql` is the canonical compiler
capability. ADR-0012 supersedes ADR-0008's compile-only attribution boundary,
and ADR-0013 records the post-two-workflow decision to remain vertical-first.
`AGENTS.md`, current designs, and specifications remain the authority for
implementation details.

## 2. Motivation

A semantic compiler answers:

```text
What physical SQL represents this governed semantic request?
```

The runtime extension problem is broader than executing one compiled query.
Metis must be able to connect to heterogeneous database families and to multiple
independent instances of each family without teaching Semantic Core about every
driver, endpoint, authentication mechanism, or connection lifecycle.

Cube provides especially relevant semantic-layer prior art. Each cube may name a
`data_source`; Cube passes that identity to `driverFactory({ dataSource })`, which
selects the database driver/configuration. This gives users a simple mental model:
a semantic object chooses a data source and runtime code chooses the driver for
that source. Trino demonstrates a complementary implementation pattern in which
registered factories support many separately configured instances.

Metis combines those lessons while preserving its own compiler/runtime split:

```text
logical ExecutionBinding
          |
          v
explicit deployment route
          |
          v
DataSource
    driver: doris
          |
          v
DriverFactory Registry
          |
          v
Doris DriverFactory
          |
          v
Executor
          |
          v
Doris instance
```

For example, one Metis deployment may need all of the following concurrently:

```text
Doris DriverFactory
  +-- production China DataSource
  +-- production US DataSource
  +-- staging DataSource

Snowflake DriverFactory
  +-- finance account DataSource
  +-- product account DataSource

DuckDB DriverFactory
  +-- local embedded DataSource
```

Adding another DataSource must be configuration-only. Adding another database
family must be a DriverFactory extension rather than a change to semantic
planning, orchestration control flow, or analytical mathematics.

That is sufficient for ordinary compiler-only integrations, but many important
analytical questions require actual data and deterministic post-query evaluation:

```text
Why did revenue decline?
Which dimension best explains the change?
Was a ratio change caused by mix or rate?
What changed the most?
Which segment should be investigated next?
```

These workflows require:

```text
semantic intent
    |
    v
one or more physical queries
    |
    v
warehouse execution
    |
    v
normalized result sets
    |
    v
deterministic analytical evaluation
    |
    v
structured analytical result
```

Stopping permanently at `SQLRenderResult` pushes orchestration and analytical
mathematics back into the Agent or every embedding application. That creates the
same class of failure RFC-0053 demonstrated for attribution compile loops:
repeated tool calls, duplicated orchestration, higher token cost, weaker
correctness, and more Agent-owned database behavior.

RFC-0052 and RFC-0053 provide the concrete trigger for this architectural
change:

- RFC-0052 proved deterministic additive/ratio metric-change attribution and
  independent multi-dimension query bundles.
- RFC-0053 showed that asking an Agent to reconstruct attribution with repeated
  ordinary compile calls was materially less reliable than one deterministic
  attribution bundle.

The next product step should not merely expose another compile endpoint. It
should establish a reusable runtime boundary that attribution and future
analytical capabilities can share.

## 3. Goals

### 3.1 Preserve the compiler core

Semantic compilation must remain independently usable, deterministic, testable,
and free of live database dependencies.

### 3.2 Make execution optional

A deployment may remain compiler-only or enable execution/runtime capabilities.
No customer should be forced to give Metis warehouse credentials merely to use
ordinary semantic compilation.

### 3.3 Support heterogeneous databases and multiple instances

Execution Runtime must use a DriverFactory extension boundary. One deployment
may register multiple DriverFactory implementations, and each DriverFactory may
back multiple independently configured DataSources that name its driver.
DriverFactory code owns the database client dependency, driver-specific
configuration schema, authentication modes, connection lifecycle, and Executor
construction; DataSource configuration identifies one concrete instance.

Warehouse execution should still present one small, engine-neutral `Executor`
contract to orchestration and analytics rather than embedding database-specific
behavior separately into attribution, comparison, or future analytical features.

### 3.4 Centralize dispatch and routing

One transport-neutral Orchestration Layer should dispatch capabilities, preserve
request/project/authentication context, route logical execution bindings through
deployment configuration to explicit named data sources, and coordinate bounded
analytical bundles. Core layers should not reproduce this control flow
independently.

### 3.5 Make analytical workflows deterministic

Multi-query analytical workflows should have explicit planners, compiled
artifacts, normalized result contracts, and deterministic evaluators. Agents
should reason over structured analytical results rather than reproduce the
mathematics themselves.

### 3.6 Keep public capabilities task-oriented

Public tools should represent complete user-facing responsibilities, not internal
pipeline stages or vague verbs.

The preferred vocabulary is:

```text
compile_sql      semantic metric query -> physical SQL
query_metrics    semantic metric query -> executed result
attribute_metric metric-change request  -> attribution result
```

For attribution this additionally means:

```text
public:   attribute_metric
internal: PlanAttribution / AttributionBundle / ExecuteBundle / EvaluateAttribution
```

Metis should not expose a public `compile_attribution` merely because attribution
internally has a compilation stage.

### 3.7 Keep the architecture extensible

The runtime must support future warehouses and future analytical capabilities
without requiring Semantic Core redesign.

### 3.8 Use familiar execution vocabulary

Configuration and extension terminology should be understandable to semantic-layer
users without requiring Metis-specific translation. `DataSource`, `Driver`, and
`DriverFactory` intentionally follow Cube's established vocabulary. Metis keeps
`ExecutionBinding` and `Executor` where they describe responsibilities that are
specific and important to its layered runtime.

## 4. Non-goals

This RFC does not propose that Metis:

- become a database or distributed SQL execution engine;
- own warehouse storage or compute;
- replace warehouse optimizers;
- accept arbitrary Agent-generated SQL as the default execution contract;
- put database drivers or credentials into Semantic Core;
- become a general-purpose workflow scheduler or Agent orchestration framework;
- select a warehouse instance heuristically from an engine, dialect, or driver;
- require runtime execution for existing ordinary compile users;
- expose every internal planner or compiled artifact as a public API;
- define all future analytical workflows now;
- introduce a generic `AnalysisPlan` abstraction before multiple real analytical
  capabilities demonstrate the need for one.

## 5. Architectural layers

### 5.1 Semantic Core

Semantic Core owns deterministic semantic resolution and physical compilation.

```text
Ossie definitions       future Catalog adapters
       \                       /
        +-- SemanticManifest Builder
                     |
                     v
          immutable SemanticManifest
                     |
                     v
             SemanticRequest
                     |
                     v
              Semantic Graph
                     |
                     v
           Analyzer / Resolver
                     |
                     v
   MetricEvaluationPlan / domain plan
                     |
                     v
              SemanticPlan
                     |
                     v
               Optimizer
                     |
                     v
                SQLPlan
                     |
                     v
       SQLRenderResult + OutputSchema
```

Semantic Core MUST NOT:

- open database connections;
- require warehouse credentials;
- execute physical queries;
- depend on result rows;
- make analytical conclusions from live data.

The public compiler-only capability is named `compile_sql` and terminates at
compiled physical output.

#### 5.1.1 SemanticManifest, Catalog, and DataSource are different concepts

The in-memory semantic read model is `SemanticManifest`.

This RFC adopts the following durable vocabulary:

```text
SemanticManifest
    immutable, versioned, project-scoped semantic read model
    built from validated Ossie definitions and derived indexes
    future input may include validated physical metadata evidence

Catalog
    external or warehouse-native metadata authority
    examples: AWS Glue Data Catalog, Apache Gravitino, Iceberg REST Catalog
    accessed through adapters; not the in-memory semantic read model

DataSource
    configured executable database or warehouse instance
    selects a Driver and is opened through its registered DriverFactory
    not necessarily a metadata Catalog
```

The distinction matters because a Catalog may describe many physical assets
without being the endpoint that executes a query, while a DataSource may execute
queries without providing a general metadata-catalog contract. Catalog adapters
and database drivers may cooperate for a particular system, but neither
interface subsumes the other.

The public/documentation concept has migrated from `Catalog snapshot` to
`SemanticManifest`. `Catalog` remains available for future external physical
metadata contracts rather than execution configuration.

MetricFlow is useful prior art for separating production semantic artifacts and
lookup/graph structures from physical warehouse metadata. Metis uses
`SemanticManifest` because it describes the complete validated semantic artifact
and is familiar to MetricFlow/dbt users. Immutability, version, digest, and
atomic publication are properties of the manifest rather than part of its public
name.

Relevant prior art includes MetricFlow's `SemanticManifest`, the AWS Glue Data
Catalog, and Apache Gravitino as a federated metadata layer supporting multiple
catalog families.

### 5.2 Execution Runtime

Execution Runtime owns controlled execution of already-compiled physical
queries against configured warehouse engines. Its primary extensibility boundary
is the DriverFactory registry: database-specific code stays behind registered
DriverFactory implementations while the rest of the runtime depends on the
common Executor contract.

```text
SQLRenderResult
     |
     v
Executor
     |
     v
Warehouse
     |
     v
ResultSet
```

The Executor MUST NOT re-resolve governed semantics or build an independent SQL
path. It executes compiler output.

The detailed type contracts are documented in the [Core runtime contract](../../specs/operations/runtime-bootstrap.md), but the separation should
remain small:

```go
type DriverFactory interface {
    // Name returns the stable driver identity selected by DataSource.driver.
    Name() string

    Supports(target CompileTarget) bool
    Validate(config DataSourceConfig) error

    Open(
        ctx context.Context,
        config DataSourceConfig,
        secrets ResolvedSecrets,
    ) (Executor, error)
}

type Executor interface {
    Execute(
        ctx context.Context,
        query SQLRenderResult,
        options ExecutionOptions,
    ) (ResultSet, error)
}
```

`Driver` is intentionally a configuration-facing/product concept rather than a
second engine-neutral Go interface competing with `Executor`. A DataSource names
a stable driver identity such as `doris`, `duckdb`, or `snowflake`; the registry
resolves that identity to one DriverFactory. The factory may wrap whatever
concrete Go client or SQL driver its database family requires.

The Orchestration Layer selects the named `DataSource`; Execution Runtime reads
its `driver`, resolves the matching DriverFactory through the registry, validates
target compatibility and driver-owned configuration, resolves `SecretRef`
values just in time, and asks the DriverFactory to open an Executor. The Executor
MUST NOT choose a warehouse instance implicitly.

DriverFactory implementations are Go-native runtime adapters registered
explicitly at bootstrap. A DriverFactory owns its concrete database-driver
library, configuration validation, authentication modes, connection lifecycle,
and Executor construction. The deployment manifest selects a registered driver
by name; it does not supply a Java class, downloadable `driver_url`, shared-library
path, or arbitrary executable code.

This naming intentionally follows Cube's `data_source` / driver /
`driverFactory` model for semantic-layer familiarity. The registry discipline is
also compatible with Trino's factory-based extension pattern. Metis calls the
configured instance `DataSource` rather than `Catalog` because Catalog is reserved
for physical metadata authorities such as Glue and Gravitino.

Execution Runtime is responsible for concerns such as:

```text
data-source / target compatibility validation
driver / DriverFactory registry resolution
driver-owned configuration validation
secret-reference resolution
query timeout
cancellation
row / byte limits
result normalization
execution identifiers
structured errors
execution telemetry
```

Connection pooling, streaming, retry policy, and advanced workload controls may
be added later without changing Semantic Core.

### 5.3 Analytics Runtime

Analytics Runtime owns domain-specific planning and deterministic evaluation for
analytical workflows that require one or more semantic queries plus returned
data. It does not own dispatch or data-source routing.

```text
Analytical Request
       |
       v
Domain Planner
       |
       v
Analytical query / SQLRenderResult bundle

normalized Result bundle
       |
       v
Domain Evaluator
       |
       v
Structured Analytical Result
```

The planner determines what governed evidence is required.

The evaluator consumes normalized results and performs deterministic analytical
mathematics.

The Agent owns natural-language intent interpretation, hypothesis generation,
and narrative explanation. It should not own arithmetic or reconciliation logic
that Metis can deterministically provide.

Analytics Runtime MUST NOT invoke warehouse drivers or choose data sources
directly. It exposes independently testable planner and evaluator operations;
the Orchestration Layer dispatches compiled queries to Execution Runtime and
returns the correlated normalized result bundle to the evaluator.

### 5.4 Orchestration Layer

The Orchestration Layer is the transport-neutral application service layer over
Semantic Core, Execution Runtime, and Analytics Runtime. REST and MCP adapters
invoke the same orchestration operations through the shared service boundary.

```text
Agent-facing capability
          |
          v
Orchestration Layer
          |
          +-- dispatch Semantic Core operation
          +-- resolve ExecutionBinding -> DataSource route
          +-- dispatch SQLRenderResult to Execution Runtime
          +-- coordinate bounded analytical bundle fan-out
          +-- correlate ResultSet entries with planned queries
          +-- dispatch Analytics Runtime evaluation
          v
Capability result
```

The Orchestration Layer owns:

- capability dispatch and runtime capability availability;
- propagation of Principal, project, request, deadline, and cancellation context;
- deterministic `ExecutionBinding` to `DataSource` route resolution;
- sequencing or bounded fan-out of physical query bundles;
- query/result correlation, budget accounting, and typed partial-failure state;
- invocation of domain planners and evaluators in the required order.

It MUST NOT:

- interpret natural language or choose an analytical investigation strategy;
- resolve semantic names, construct semantic plans, or rewrite physical SQL;
- implement warehouse drivers or inspect credential material;
- perform domain analytical mathematics;
- guess a data source when routing is missing or ambiguous;
- introduce behavior differences between REST and MCP.

The exact package boundary is deferred to RFC-0056. The current `app/service/semantic`
boundary is the starting point; a focused orchestration package should be added
only if implementation pressure proves that the service layer cannot keep this
responsibility cohesive.

## 6. Dependency direction

Semantic Core and Execution Runtime are siblings separated by the compiler
artifact contract. Execution Runtime consumes `SQLRenderResult`; it does not
depend on semantic resolution, planning, or compilation behavior.

The dependency direction is:

```text
REST / MCP / embedded adapters
              |
              v
      Orchestration Layer
       /       |       \
      v        v        v
Semantic   Execution   Analytics
  Core      Runtime    Runtime
    |          |          |
    |          |          +----> Semantic Core
    v          v
compiler artifact contracts
SQLRenderResult / OutputSchema
```

Ordinary `query_metrics` follows this sibling shape: the Orchestration Layer
invokes Semantic Core, receives one canonical `SQLRenderResult`, resolves the
request's explicit data-source route, and passes the artifact and selected
data-source identity to Execution Runtime. Analytical planner code may depend on
Semantic Core to produce governed bundles, but it MUST NOT call Execution Runtime
directly. Execution Runtime MUST NOT import or invoke manifest, resolver, planner,
optimizer, or semantic-engine behavior.

Semantic Core MUST remain buildable and testable without Execution Runtime.
Execution Runtime MUST remain usable for ordinary compiled queries without
Analytics Runtime. Compiler-only deployments MUST exclude Execution Runtime
without changing Semantic Core behavior. None of the three runtimes may depend
on transport adapters or call back into the Orchestration Layer.

## 7. Deployment modes

The proposed architecture supports three logical capability modes.

### 7.1 Compiler-only

```text
SemanticRequest
      |
      v
Orchestration Layer
      |
      v
compile_sql
      |
      v
SQLRenderResult
```

No warehouse credentials or database connectivity are required.

This preserves the strongest isolation model and current integration style.

Compiler-only mode supports ordinary semantic compilation. It does not imply
that every Analytics Runtime workflow must have an equivalent public compile-only
endpoint.

In particular, attribution does **not** require a public compile-only form.

### 7.2 Runtime

```text
SemanticRequest
      |
      v
Orchestration Layer
      |
      v
query_metrics
      |
      v
SQLRenderResult
      |
      v
DataSource
      |
      v
DriverFactory -> Executor
      |
      v
QueryResult
```

`query_metrics` preserves the same compiler authority as `compile_sql`.

### 7.3 Analytics runtime

```text
AnalyticalRequest
      |
      v
Orchestration Layer
      |
      +-- Analytics Planner
      +-- Semantic Core compilation
      +-- DataSource routing / bounded dispatch
      +-- Analytics Evaluator
      |
      v
AnalyticalResult
```

This supports complete deterministic analytical capabilities such as
attribution.

These are capabilities, not mutually exclusive products. One server may expose
`compile_sql`, `query_metrics`, and analytical tools together when policy permits.

## 8. SQLRenderResult is the execution boundary

The compiler output is the only accepted semantic-to-execution handoff.

```text
compile_sql
   |
   v
SQLRenderResult
   |
   +-----------------> external caller executes
   |
   +-----------------> Metis Execution Runtime executes
```

There MUST NOT be separate SQL-building implementations for ordinary compile and
execute paths.

For ordinary runtime queries:

```text
query_metrics
    |
    v
Orchestration Layer
    |
    +-- Compile(...)
    |       |
    |       v
    |   SQLRenderResult
    |
    +-- resolve ExecutionBinding -> DataSource
    |
    +-- resolve DataSource.driver -> DriverFactory
    |
    v
Executor.Execute(...)
```

The SQL returned by `compile_sql` and the SQL executed by `query_metrics` must
originate from the same canonical compiler artifact for the same resolved
request and target.

The names describe different product responsibilities, not different compiler
pipelines.

## 9. Internal analytical compilation boundary

Analytical workflows may require domain-specific planning that produces multiple
physical queries.

That compiled bundle is an important internal architecture boundary, but an
internal boundary does not automatically become a public API.

For attribution:

```text
AttributionRequest
      |
      v
Attribution Planner
      |
      v
AttributionBundle
      |
      +-- SQLRenderResult(total/evidence as required)
      +-- SQLRenderResult(region)
      +-- SQLRenderResult(channel)
      +-- SQLRenderResult(product)
```

`AttributionBundle` remains useful for:

- deterministic planner/compiler tests;
- fingerprints and conformance evidence;
- separation of planning from execution;
- executor orchestration;
- evaluator fixtures and diagnostics;
- future embedded/internal library use when appropriate.

It is not, by itself, a reason to add a public `compile_attribution` tool or REST
endpoint.

This distinction prevents implementation structure from unnecessarily expanding
the Agent-facing vocabulary.

## 10. Attribution as the first Analytics Runtime capability

Attribution is the first reference implementation of Analytics Runtime.

The complete public path is:

```text
attribute_metric
      |
      v
Orchestration Layer
      |
      +-- Attribution Planner / Semantic Core
      |        -> AttributionBundle
      |
      +-- DataSource routing / Execution Runtime
      |        -> AttributionResultBundle
      |
      +-- Attribution Evaluator
      |        -> AttributionResult
      v
AttributionResult
```

The existing deterministic attribution bundle from RFC-0052 is therefore not a
throwaway benchmark optimization. It becomes the stable **internal** boundary
between attribution planning and execution.

The earlier compile-only attribution proposals in PR #493 and PR #494 are
superseded because they treated that bundle as the final public Metis product
boundary.

Their durable semantic decisions remain useful and should feed RFC-0057:

```text
bounded multi-dimension request
independent per-dimension planning
canonical ordering
shared population/filter semantics
typed additive/ratio evidence
reconciliation semantics
explicit undefined zero-denominator states
```

## 11. Public attribution surface

RFC-0057 MUST expose one complete attribution capability:

```text
attribute_metric
    AttributionRequest
        -> AttributionResult
```

There is no public:

```text
compile_attribution
```

for MCP, REST, or the primary Agent-facing tool surface.

Internally, `attribute_metric` is still implemented compositionally:

```text
attribute_metric
      |
      v
Orchestration Layer
      |
      +-- Plan / Compile Attribution
      |        -> AttributionBundle
      |
      +-- Route / Execute Bundle
      |        -> normalized ResultBundle
      |
      +-- Evaluate Attribution
               -> AttributionResult
```

The internal stages MUST remain independently testable even though only the
complete analytical operation is public.

This has three benefits:

1. The public vocabulary stays small and intent-oriented.
2. Agents do not need to understand or orchestrate Metis implementation stages.
3. Metis can change internal bundle/execution details without unnecessarily
   versioning an additional public transport contract.

## 12. Attribution Evaluator boundary

The Attribution Evaluator must be database- and dialect-independent.

Its conceptual input is:

```text
AttributionBundle
+
normalized ResultBundle
```

Its output should include deterministic structured evidence such as:

```text
baseline/current totals
delta and percent change
per-dimension segment effects
contribution/reconciliation
entry / continuing / exit states
ratio mix / rate effects
undefined-state diagnostics
warnings
```

The evaluator must be testable entirely from fixture result bundles without a
live database.

The Agent may explain business meaning or choose a further investigation, but
arithmetic, undefined-state handling, reconciliation, and other supported
attribution mathematics should remain deterministic Metis behavior.

## 13. Future analytical capabilities

This architecture must not be attribution-specific.

Potential future capabilities include:

```text
comparison
period-over-period analysis
top movers
anomaly explanation
funnel
retention
cohort analysis
distribution change
trend decomposition
root-cause investigation workflows
```

This RFC does not approve those capabilities individually.

A generic Analytics Runtime IR should be introduced only after at least two real
capabilities demonstrate stable shared structure. Attribution and comparison are
the preferred first two verticals for that validation.

The same public-surface rule should apply to future analytical capabilities:
internal planner/evaluator stages do not automatically deserve public tools.

## 14. Data sources, drivers, driver factories, secret references, and routing

> **Superseded detail:** [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and ADR-0010 replace this section's
> `ExecutionBinding -> route -> DataSource -> Driver` chain with root Project
> Registration -> DataSource, `DataSource.type -> BackendRegistry`, and one
> selected Renderer authority. The remainder of this section is retained only
> as proposal history and MUST NOT guide new implementation.

Execution configuration must remain outside Semantic Core.

`ExecutionBinding` retains its existing narrow meaning: project-scoped logical
placement metadata used to resolve an engine and compile target. It MUST NOT
contain endpoints, ports, usernames, passwords, TLS material, connection-pool
settings, credential references, or query-execution behavior.

The central runtime extension model is DataSource plus Driver/DriverFactory.
Secret resolution and routing support that model; they are not the product
abstraction being optimized.

The vocabulary intentionally follows Cube where doing so improves familiarity:

1. **DataSource** is one named, deployment-configured database or warehouse
   instance. It names exactly one `driver`.
2. **Driver** is the stable database-family identity selected by a DataSource,
   for example `doris`, `duckdb`, or `snowflake`.
3. **DriverFactory** is registered Go code for one Driver. It owns the concrete
   client/driver dependency, supported compile targets/capabilities, configuration
   schema, authentication modes, connection lifecycle, and Executor construction.
4. **Executor** is the small engine-neutral runtime contract returned by a
   DriverFactory and used to execute canonical `SQLRenderResult` artifacts.
5. **SecretRef** is indirection from a driver-defined sensitive configuration
   field to an external secret provider. It is not a new credential domain model.
6. A deployment **route** maps a project-scoped logical `ExecutionBinding` to one
   DataSource. Route is orchestration configuration, not a new semantic asset or
   independently addressable public object.

DataSource is configuration; DriverFactory is implementation; Executor is the
opened engine-neutral execution boundary. They MUST NOT be collapsed.

Many DataSources may use the same driver/DriverFactory to reach different
development, staging, production, regional, tenant-isolated, or workload-isolated
instances. DataSource identities are stable and unique within one deployment.
Callers select an instance through an authorized logical binding and route; they
do not inject driver names, endpoints, data-source identities, or secret
references into semantic requests.

```text
one deployment
    |
    +-- 0..N DriverFactory implementations
    |       |
    |       +-- one stable Driver identity per factory
    |       +-- 0..N DataSources selecting that driver
    |
    +-- 0..N project/binding routes
            |
            +-- exactly one DataSource per route
```

Consequently:

- registering `postgres`, `mysql`, `doris`, `snowflake`, or future DriverFactory
  code extends the supported database families;
- adding `doris-prod-cn`, `doris-prod-us`, or another DataSource extends concrete
  instances without new runtime code;
- driver-specific configuration may differ substantially between database
  families without creating engine-specific fields in the shared DataSource
  envelope;
- credentials, TLS, database-driver options, and session parameters are supporting
  driver configuration concerns, not separate semantic placement concepts.

```text
ExecutionBinding
    binding identity
    engine
    dialect / target

Driver
    stable driver identity
    examples: doris, duckdb, snowflake

DriverFactory (registered Go code)
    driver identity
    supported engine / compile targets / capabilities
    driver-owned configuration schema
    concrete database client / driver implementation
    Executor factory

DataSource (deployment configuration)
    data-source identity
    driver identity
    driver-specific configuration values
    SecretRef values in driver-defined sensitive fields
    bounded execution policy

SecretRef (secure-value indirection)
    provider identity
    provider-specific lookup key or non-secret parameters

route (deployment mapping, not a domain object)
    project identity
    binding identity
    data-source identity
```

Data-source routing occurs only after the request project and logical
`ExecutionBinding` have resolved through the existing deterministic precedence.
The Orchestration Layer resolves the project-scoped route to exactly one existing
`DataSource`. Execution Runtime resolves the DataSource's driver through the
DriverFactory registry and validates the DataSource with that driver's schema.
Engine or dialect equality is compatibility validation, not data-source
selection: two data sources using the same driver remain distinct runtime choices
and MUST NOT be chosen by heuristic or load-order accident.

There is no implicit default project, cross-project data-source fallback, or
endpoint selection based only on engine/dialect/driver. An unknown driver,
missing DriverFactory, invalid DataSource, or missing, incompatible, or ambiguous
route fails explicitly.

### 14.1 Prior art and derived common structure

This model is derived from existing systems rather than from Metis-specific
naming preferences:

| System | Driver/factory implementation | Configured instance | Secret/auth behavior | Consumer selection |
| --- | --- | --- | --- | --- |
| Cube | database driver selected by `driverFactory({ dataSource })` | named `data_source`; Cube Cloud also exposes configured Data Sources | deployment settings/environment variables or driver-specific configuration | cube `data_source` and request context are passed to `driverFactory` |
| Trino | plugin `ConnectorFactory` registered by connector name | named Catalog property set | environment secret substitution and connector credential providers | SQL catalog identity |
| DuckDB | extension registers database and secret types/providers | `ATTACH` target plus typed connection options | named typed Secrets with pluggable providers | attached database identity |
| Snowflake clients | installed language driver/connector | named connection configuration | driver-defined authenticator parameters | connection name |
| Grafana | installed data-source plugin | configured data-source instance | plugin-defined normal and secure configuration fields | data-source identity or UID |
| JDBC | driver-vendor implementation | `DataSource` representing a physical source and producing live `Connection` objects | implementation-defined properties | registered DataSource name |

Cube is the primary terminology precedent for Metis because both products expose
a semantic layer over multiple database families. Cube explicitly documents that
each cube can set a `data_source` and that the value is passed to
`driverFactory()`. Its dynamic-model documentation shows `driverFactory`
selecting MySQL or Postgres drivers based on `dataSource`. Metis intentionally
reuses those terms while keeping routing and Executor contracts explicit.

The relevant upstream contracts include:

- [Cube Data Sources](https://docs.cube.dev/configuration/data-sources),
  [cube `data_source`](https://docs.cube.dev/reference/data-modeling/cube), and
  [dynamic `driverFactory`](https://docs.cube.dev/docs/data-modeling/dynamic/javascript);
- [Cube multiple data sources](https://docs.cube.dev/recipes/pre-aggregations/joining-multiple-data-sources)
  and [tenant-specific data sources](https://docs.cube.dev/recipes/configuration/multiple-sources-same-schema);
- [Trino ConnectorFactory configuration](https://trino.io/docs/current/develop/connectors.html)
  and [Catalog concepts](https://trino.io/docs/current/overview/concepts.html);
- [DuckDB Secrets Manager](https://duckdb.org/docs/current/configuration/secrets_manager);
- [Snowflake named connections](https://docs.snowflake.com/en/developer-guide/snowflake-cli/connecting/configure-connections);
- [Grafana data sources](https://grafana.com/docs/grafana/latest/datasources/);
- [JDBC DataSource](https://docs.oracle.com/en/java/javase/26/docs/api/java.sql/javax/sql/DataSource.html).

`Catalog` is not reused for this runtime concept even though Doris and Trino use
it for configured connector instances. Metis reserves Catalog for genuine
physical metadata authorities and integrations such as Glue and Gravitino.
`Connection` is kept for a live database-driver/session or pool handle.
`DataSource` most directly matches the configured-instance meaning used by Cube,
Grafana, and common database APIs.

### 14.2 `metis.yaml` owns DataSource configuration

Concrete warehouse connection information belongs in named `data_sources`
entries in the root deployment `metis.yaml` passed to `metis serve --config`.
It MUST NOT be added to Ossie documents, canonical semantic models, datasets,
fields, or logical `ExecutionBinding` values. Referenced project manifests
continue to own project identity, models, and logical bindings only.

A runtime-enabled deployment has the conceptual shape:

```yaml
projects:
  - path: ./projects/finance.yaml
  - path: ./projects/growth.yaml
  - path: ./projects/product.yaml
  - path: ./projects/local-lab.yaml

data_sources:
  - id: doris-prod-cn
    driver: doris
    config:
      address: doris-prod-cn.internal:9030
      database: analytics
      parameters:
        parseTime: "true"
      username: metis_reader
      password:
        secret_ref:
          provider: env
          key: METIS_DORIS_PROD_CN_PASSWORD
    policy:
      query_timeout: 30s
      max_rows: 100000

  - id: doris-prod-us
    driver: doris
    config:
      address: doris-prod-us.internal:9030
      database: analytics
      parameters:
        parseTime: "true"
      username: metis_reader
      password:
        secret_ref:
          provider: env
          key: METIS_DORIS_PROD_US_PASSWORD
    policy:
      query_timeout: 30s
      max_rows: 100000

  - id: snowflake-product
    driver: snowflake
    config:
      account: acme-product
      warehouse: product_wh
      database: product_analytics
      role: metis_reader
      username: metis_service
      password:
        secret_ref:
          provider: env
          key: METIS_SNOWFLAKE_PRODUCT_PASSWORD
    policy:
      query_timeout: 45s
      max_rows: 100000

  - id: duckdb-local
    driver: duckdb
    config:
      path: ./data/local.duckdb
      read_only: true
    policy:
      query_timeout: 10s
      max_rows: 10000

execution:
  routes:
    - project: finance
      execution_binding: doris-prod
      data_source: doris-prod-cn
    - project: growth
      execution_binding: doris-prod
      data_source: doris-prod-us
    - project: product
      execution_binding: snowflake-product
      data_source: snowflake-product
    - project: local-lab
      execution_binding: duckdb-local
      data_source: duckdb-local
```

The exact field spelling is documented in the [Core runtime contract](../../specs/operations/runtime-bootstrap.md), but the ownership and safety
rules are fixed by this accepted architecture:

- `data_sources` is deployment-scoped, data-source IDs are unique, and every
  data source names exactly one registered driver;
- every driver identity must resolve to exactly one registered DriverFactory;
- the DriverFactory owns the schema, defaults, authentication modes, secret
  annotations, and strict validation for its driver's `config` object;
- `execution.routes` is keyed by explicit project plus logical binding and
  resolves to exactly one data source;
- a data source may be shared by multiple projects only through separate explicit
  routes; sharing is never inferred from engine, dialect, driver, or name;
- every route must reference a loaded project, a binding in that project, and a
  data source whose DriverFactory supports the binding's engine/compile target;
- duplicate data-source IDs, duplicate route keys, unknown drivers, missing
  DriverFactories, missing references, schema-invalid config, and incompatible
  binding/data-source pairs fail strict startup validation;
- referenced project manifests MUST NOT define data sources or physical
  connection properties, so the same semantic project can be deployed against
  different warehouse instances without modification;
- compiler-only deployments may continue to load a project manifest directly;
  enabling Execution Runtime requires a deployment `metis.yaml`, including for a
  deployment containing only one project.

Plaintext passwords, access tokens, private keys, and other secret material MUST
NOT appear in `metis.yaml`. DriverFactory schemas mark secret-bearing fields and
require those values to be `SecretRef`, never plain strings. Non-secret fields
such as host, port, database, warehouse, role, account, username, protocol, and
bounded driver parameters remain ordinary typed configuration.

Metis Core standardizes only the `SecretRef` envelope and provider registry. It
does not standardize one global `basic|oauth|key_pair|workload_identity` union:
authentication modes and their required fields belong to the DriverFactory that
actually implements them.

This design does not require a top-level named `Secret` object. Driver-owned
external-value resolution supplies credentials without adding a semantic-model
identity or configuration authority.

Secret values are resolved only inside Execution Runtime after project
authorization, route resolution, DriverFactory/config validation, and limit
checks. Resolved values MUST NOT enter semantic requests, plans, `SQLRenderResult`,
error details, logs, metrics, traces, or cached configuration snapshots. The
provider registry should permit sources such as:

```text
environment variables
mounted files or keystores
cloud secret managers
workload identity / credential chains
short-lived token providers
customer-provided SecretResolver implementations
```

Exact contracts belong to [Core runtime contract](../../specs/operations/runtime-bootstrap.md).

## 15. Security boundary

Execution expands the trust boundary and therefore remains optional.

Runtime designs MUST support least-privilege and read-only execution where the
warehouse permits it.

The execution layer must eventually provide explicit controls for:

```text
credential isolation
project/tenant isolation
query timeout
row limits
byte limits
cancellation
audit identity
allowed execution targets
allowed data sources and routes
```

Compiler-only deployments remain fully supported for environments where Metis
must never receive warehouse credentials.

`attribute_metric` is available only when an execution capability is configured
for the active project/runtime. It must fail with a typed capability/configuration
error rather than silently falling back to returning SQL.

Authorization MUST be checked against the request project, resolved
`ExecutionBinding`, and routed `DataSource` before secret resolution or driver
invocation. Possession of a valid data-source identifier alone does not grant
cross-project execution access.

## 16. Resilience

A compiler interacts primarily with deterministic local state. A runtime
interacts with unreliable external systems.

Execution therefore makes resilience a first-class product concern.

Follow-on runtime work must account for:

```text
timeouts
cancellation
connection failures
credential expiry
warehouse overload
partial analytical bundle failure
result-size explosion
resource cleanup
backpressure
retry safety
```

Analytical workflows MUST NOT silently present partial execution as a complete
analytical conclusion.

The runtime should fail explicitly and preserve enough structured evidence to
identify which physical query or analytical stage failed.

## 17. Observability

RFC-0036 established the current runtime observability foundation. Execution and
analytics extend that contract.

The architecture should make it possible to observe at least:

```text
compile latency
execution latency
warehouse latency
orchestration latency
data-source routing failure
physical query count
rows returned
bytes returned
execution timeout / cancellation / error
analytics bundle fan-out
analytics evaluation latency
partial failure
```

For analytical requests, operators should eventually be able to answer:

```text
How many warehouse queries did this request produce?
Which query failed?
Was any result truncated?
How expensive was the workflow?
Did attribution reconcile successfully?
```

Detailed metric names and cardinality constraints belong in the implementation
RFCs/specifications. Telemetry MUST NOT record connection endpoints or secret
references; data-source identity may appear only where the observability contract
can keep it bounded and non-secret.

## 18. Public API direction

The public capability direction is:

```text
compile_sql
    SemanticQuery
        -> SQLRenderResult

query_metrics
    SemanticQuery
        -> QueryResult

attribute_metric
    AttributionRequest
        -> AttributionResult
```

These names are semantic contracts, not merely cosmetic renames.

### 18.1 `compile_sql`

`compile_sql` communicates that the operation terminates at physical SQL and does
not access warehouse result data. It replaces the overly generic public name
`compile`.

Internally, packages and service methods may continue to use compiler-oriented
names such as `Compile` where those names are unambiguous. This RFC governs the
public tool/API vocabulary.

### 18.2 `query_metrics`

`query_metrics` communicates that callers submit a governed metric query, not raw
SQL. It replaces the proposed `run_query` name, which could be interpreted as an
arbitrary SQL execution primitive.

`query_metrics` MUST compile through the same canonical compiler path used by
`compile_sql` before executing the resulting `SQLRenderResult`.

### 18.3 `attribute_metric`

`attribute_metric` communicates a higher-order analytical operation over one
governed metric and explicit comparison periods/dimensions. It owns planning,
physical execution, and deterministic evaluation behind one public capability.

The existing unprefixed MCP naming convention from RFC-0051 remains the default.
REST and MCP projections of the same capability must continue to share one
transport-neutral application/service operation.

## 19. Compatibility and migration of `compile`

Unlike `query_metrics`, which is new, `compile_sql` renames an existing public
capability currently exposed as `compile`.

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) and RFC-0056 implementation work MUST therefore choose and document an explicit
migration strategy rather than accidentally maintaining two permanent synonyms.

The preferred direction is:

```text
canonical name: compile_sql
legacy name:    compile   (temporary compatibility alias only if required)
```

If compatibility with already published clients requires an alias, the alias
must be time-bounded/deprecated and route to exactly the same service operation.
It must not create a second compiler path or divergent schema.

If Metis' current versioning policy permits a clean pre-stable breaking rename,
implementation may remove `compile` directly after updating tests, specs,
examples, and Agent instructions. The implementation RFC/PR should make that
choice explicit based on the release compatibility contract at that time.

There is no analogous migration concern for `run_query`, because RFC-0056 has not
yet established it as a production public capability; its canonical name should
start as `query_metrics`.

## 20. Relationship to existing RFCs

The supersessions in this section are effective. [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and RFC-0056 through RFC-0064 and
ADR-0010 through ADR-0013 completed the required implementation and
current-contract updates.

### RFC-0051

RFC-0051's progressive semantic tools and bounded discovery principles remain in
force. Its compile-only product boundary is no longer a permanent constraint on
the optional runtime layers.

Its former public `compile` naming is superseded by canonical `compile_sql`.

Runtime capabilities should be added only when they represent clear, bounded
responsibilities rather than expanding generic tools indiscriminately.

### RFC-0052

RFC-0052's deterministic attribution semantics, independent dimension planning,
additive/ratio decomposition, undefined states, and conformance evidence remain
authoritative.

Only the permanent compile-only ownership boundary changed. RFC-0052's compiled
attribution bundle is an internal input to optional runtime execution.

### RFC-0053

RFC-0053 is retained as evidence that Agent-owned repeated compile orchestration
is an unreliable boundary for deterministic analytical workflows.

It motivates moving orchestration into governed runtime capabilities rather than
merely adding more primitive tools.

### ADR-0008 and current contracts

ADR-0012 supersedes ADR-0008's compile-only execution and public-interface
boundary while preserving its deterministic attribution semantics. ADR-0013
records that two real workflows do not automatically authorize a generalized
analytical IR.

The same implementation sequence MUST reconcile, rather than silently ignore,
the current repository contract:

```text
AGENTS.md project identity
AGENTS.md ExecutionBinding invariant
AGENTS.md compile-only / no-engine-execution invariants
AGENTS.md product anti-goals
docs/specs/glossary.md
docs/specs/operations/runtime-bootstrap.md
docs/specs/semantic/metric-change-attribution.md
docs/design/operations/runtime-bootstrap.md
docs/design/semantic/pipeline.md
public REST / MCP specifications and examples
```

The intended boundary change is narrow:

- Semantic Core, SQLPlan, renderers, and `ExecutionBinding` remain free of
  connectivity, credentials, execution, and result handling;
- the Orchestration Layer may resolve explicit project-scoped routes from logical
  bindings to `DataSource` identities;
- the optional Execution Runtime may use the selected `DataSource`, resolve its
  driver through the registered DriverFactory, resolve secret references, invoke
  the warehouse database driver, and normalize bounded results;
- Metis does not expose arbitrary raw-SQL execution, a SQL proxy, a JDBC/MySQL
  serving endpoint, or a general-purpose credential-management product;
- project isolation, explicit target resolution, shared authentication, and
  transport-neutral service behavior remain unchanged.

Until those replacements are accepted and landed, the existing global
compile-only statements remain current truth.

## 21. Follow-on RFC sequence

### SemanticManifest terminology migration

Phase 1 migrated the in-memory semantic concept from `catalog.Snapshot` to
`manifest.SemanticManifest` before runtime execution contracts could make the
ambiguity harder to remove. The change updated the package responsibility map,
bootstrap/design diagrams, comments, tests, and public documentation together
without changing semantic behavior.

The migration reserves `Catalog` for a later adapter contract over physical
metadata authorities such as Glue, Gravitino, Iceberg catalogs, and
warehouse-native catalogs. It MUST NOT make those external systems a live
request-time dependency of deterministic semantic resolution; validated catalog
evidence enters a newly built SemanticManifest before it becomes current.

### Execution Runtime Contracts

Owns:

```text
Executor
ResultSet
ExecutionOptions
ExecutionError
ExecutionBinding
DataSource
Driver / DriverFactory Registry
SecretRef / SecretResolver registry
data-source routing contract
deployment metis.yaml data-source / route grammar
strict bootstrap validation
limits / cancellation contract
```

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) removes the existing logical `ExecutionBinding` contract. Its
conformance cases MUST include at least two DataSource values using the same
Backend/DriverFactory, plus DataSources using at least two different Backend
implementations. Tests must prove deterministic project resolution and direct
Project Registration -> DataSource selection without a second route authority.

It must also update the runtime-bootstrap contract and test relative deployment
loading, duplicate IDs/routes, unknown driver IDs, missing DriverFactories,
missing references, driver-owned schema validation, binding/data-source
compatibility, forbidden data-source fields in referenced project manifests, and
rejection of plaintext values in secret-annotated fields.

Recommended initial execution targets are DuckDB and Doris/MySQL protocol so the
contract is exercised by both deterministic local tests and a real remote
warehouse path.

### RFC-0056: Runtime Metric Query Execution

Owns:

```text
query_metrics
Compile -> SQLRenderResult -> Execute
Orchestration Layer capability dispatch
ExecutionBinding -> DataSource routing
shared service boundary
REST/MCP projection
result-size guardrails
execution observability
compile -> compile_sql public-name migration completion as applicable
```

It must prove there is no second query compiler path.

### RFC-0057: Production Metric Attribution Runtime

Owns:

```text
attribute_metric
internal Attribution Planner / AttributionBundle
Attribution Evaluator
AttributionResult
bundle execution behavior
partial failure semantics
Agent-facing stopping guidance
```

It should carry forward durable request/bundle semantics from the superseded PR
#493 and #494 instead of recreating them from scratch.

## 22. Implementation order

The intended priority order is:

```text
1. Accept RFC-0054 architecture and public vocabulary
2. Add the superseding ADR and an explicit current-contract migration plan
3. Migrate the current semantic Catalog terminology to SemanticManifest and
   reserve Catalog for external physical metadata authorities
4. Align execution vocabulary with DataSource / Driver / DriverFactory before
   Core runtime contracts turns it into production contracts
5. Update AGENTS.md from a global execution prohibition to the accepted layered
   boundary before implementation crosses that red line
6. Core runtime contracts Execution Runtime Contracts
7. reference DuckDB + Doris DriverFactories and execution documentation
8. RFC-0056 query_metrics + compile_sql migration and public documentation
9. Attribution Evaluator
10. RFC-0057 attribute_metric, superseding ADR-0008, and attribution spec updates
11. reconcile the final design, glossary, and contract inventory
12. second analytical vertical, preferably comparison
13. only then evaluate shared generic Analytics Runtime primitives
```

This sequence deliberately avoids premature abstraction while preventing
short-term compile-only interfaces from hardening into a long-term product
constraint.

## 23. Acceptance criteria

RFC-0054 is successfully realized when the architecture can demonstrate all of
the following:

1. Semantic Core reads one immutable `SemanticManifest` and has no live database,
   external Catalog, or credential dependency during request processing.
2. Runtime execution consumes canonical `SQLRenderResult` output rather than
   rebuilding SQL independently.
3. Root Project Registration optionally selects one deployment-scoped
   DataSource directly; no `ExecutionBinding` or parallel route map remains.
4. Every DataSource names exactly one type, and every executable type resolves
   exactly one Backend containing SQLDialect, Renderer, and DriverFactory.
5. Multiple DataSources using the same Backend, and DataSources using different
   Backends, coexist and resolve deterministically without target/dialect/driver
   guessing.
6. Concrete data-source configuration is loaded from the separately referenced
   deployment DataSource registry; project manifests and Ossie assets contain no
   connection or secret configuration. `Catalog` remains reserved for external
   physical metadata authority rather than execution-instance configuration.
7. Bootstrap fails closed for duplicate, missing, ambiguous, unknown-type,
   missing-Backend/factory, schema-invalid DataSources, invalid project
   references, and plaintext values in secret-annotated fields.
8. One transport-neutral Orchestration Layer owns capability dispatch, data-source
   routing, bounded bundle scheduling, cancellation, and result correlation.
9. Semantic Core, Execution Runtime, and Analytics Runtime do not call back into
   the Orchestration Layer, and Analytics Runtime does not call Execution Runtime
   directly.
10. Execution Runtime does not depend on semantic resolver, planner, optimizer,
    manifest, Catalog, or semantic-engine behavior.
11. Runtime execution can be disabled entirely.
12. At least two database-family DriverFactory implementations can satisfy the
    same Executor contract without database-specific orchestration branches.
13. `query_metrics` uses exactly the same compiler path as `compile_sql`.
14. The canonical public compiler capability is `compile_sql`; any temporary
    `compile` compatibility alias has no independent behavior.
15. `query_metrics` accepts governed semantic metric requests, not arbitrary raw
    SQL execution requests.
16. Attribution planning and attribution execution use the same internal
    `AttributionBundle`.
17. Attribution evaluation can be tested from fixture result bundles without a
    database.
18. `attribute_metric` is the only primary public attribution capability and
    fails explicitly when execution is unavailable.
19. Adding a new warehouse does not require modifying attribution mathematics.
20. At least two analytical workflows can reuse the same Execution Runtime before
    Metis introduces a generalized analytical IR.
21. Production runtime behavior exposes explicit limits, cancellation, errors,
    and observability.
22. Compiler-only deployments remain a first-class supported product mode.
23. A superseding ADR and all affected current contracts are updated before the
    runtime architecture is described as implemented or current truth.

## 24. Strategic direction

Metis should not optimize only for today's semantic-to-SQL requirements.

As analytical Agents become more capable, they increasingly need deterministic
infrastructure not only to discover governed semantics and compile SQL, but also
to perform multi-query analytical workflows safely, efficiently, and
reproducibly.

The long-term evolution is therefore:

```text
Semantic Compiler
        |
        v
Semantic Runtime
        |
        v
Analytical Runtime
```

without sacrificing the clean compiler foundation that makes Metis trustworthy.

The product should continue to optimize for:

- **Ease of use:** fewer Agent calls and less database-specific orchestration.
- **Familiarity:** DataSource / Driver / DriverFactory vocabulary maps cleanly to
  mature semantic-layer practice while Metis-specific boundaries stay explicit.
- **Extensibility:** stable boundaries for new warehouses and analytical
  capabilities.
- **Flexibility:** compiler-only, runtime, embedded, server, REST, MCP, and
  application-integrated modes can coexist.
- **Resilience:** external execution failure, limits, cancellation, and partial
  analytical workflows are explicit parts of the runtime model.
- **Determinism:** semantic resolution, planning, analytical mathematics, and
  reconciliation remain deterministic wherever possible.

This RFC establishes that direction as an accepted long-term architectural
commitment, not a short-term implementation detail for attribution. Until the
staged implementation and current-contract updates land, it records the target
and migration requirements without changing current runtime truth.
