# RFC-0059: Production Execution Runtime Resource Lifecycle and Concurrency Control

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-30
- **Last updated:** 2026-08-30
- **Scope:** Execution Runtime lifecycle, per-DataSource runtime resources, connection/client pooling, execution admission control, bounded queues, DriverFactory and Executor ownership, secret-resolution lifecycle, graceful shutdown, concurrency safety, observability, race/load acceptance
- **Supersedes:** none
- **Related:** RFC-0054, RFC-0056, RFC-0058, ADR-0010

## 1. Summary

This RFC defines the production resource and concurrency model for Metis Execution Runtime.

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) defines runtime authority:

```text
Project
    -> DataSource
    -> DataSource.type
    -> Backend
         +-> Renderer
         `-> DriverFactory
```

RFC-0056 composes that authority into the public governed metric-query workflow:

```text
SemanticQuery
    -> canonical semantic compilation
    -> CompiledQuery
    -> ExecutionRuntime
    -> ResultSet
```

Those RFCs intentionally do not fully define process-level connection/client ownership, concurrent execution admission, secret reuse, shutdown, or concurrency-safety contracts. Without those rules, a conforming implementation could accidentally create and destroy one database pool per query, resolve remote credentials on every request, or allow an Agent fan-out to send an unbounded number of simultaneous warehouse queries.

This RFC closes that gap.

The target architecture is:

```text
                    process lifetime
                         |
                         v
                +------------------+
                | ExecutionRuntime |
                +---------+--------+
                          |
              one runtime entry per
               configured DataSource
                          |
             +------------v-------------+
             |    DataSourceRuntime      |
             |                           |
             | Backend                   |
             | shared client / pool      |
             | execution admission gate  |
             | credential state          |
             | lifecycle state           |
             +------------+-------------+
                          |
               request-scoped leases
                          |
          +---------------+---------------+
          |               |               |
          v               v               v
      Executor        Executor        Executor
      request A       request B       request C
          |               |               |
          +---------------+---------------+
                          |
                          v
                 shared DB/client pool
                          |
                          v
                       warehouse
```

The key decisions are:

1. `ExecutionRuntime` is process-scoped and initialized once per server runtime, not once per query.
2. `BackendRegistry` and `DataSourceRegistry` are build/validate/freeze structures and are read-only after bootstrap.
3. Each configured executable DataSource has at most one process-scoped live `DataSourceRuntime` per Metis process.
4. `DataSourceRuntime` owns reusable driver/client resources such as `*sql.DB`, SDK clients, sessions, or embedded-engine handles.
5. Heavy live resources are lazily initialized on first execution by default.
6. Concurrent first use of one DataSource coalesces into one initialization attempt rather than a connection storm.
7. `Executor` remains request-scoped and borrows process-scoped resources; it does not own or destroy the shared DataSource pool/client.
8. `Executor.Close()` releases request/session resources only. Process-level destruction is owned by `DataSourceRuntime.Close()` and ultimately `ExecutionRuntime.Close()`.
9. Every execution-enabled DataSource has a positive `max_concurrency` execution policy; omission uses the bounded default of `100`.
10. Waiting for capacity is bounded. Infinite execution queues are forbidden.
11. Admission control is per DataSource and occurs before acquiring a request execution resource and before issuing the physical query.
12. Capacity exhaustion is a first-class typed execution outcome distinct from query timeout.
13. Runtime extension points used concurrently have explicit thread-safety contracts.
14. Secret lookup must not inherently become one external secret-provider request per query.
15. Graceful shutdown stops new admission, drains or cancels bounded in-flight work, and closes shared DataSource resources exactly once.
16. Production acceptance requires race tests and concurrent load evidence, not only sequential correctness tests.

The core invariant is:

> **Request-scoped query execution may borrow process-scoped resources, but it must never create an unbounded resource lifecycle, an unbounded execution queue, or an independently owned connection pool per query.**

## 2. Motivation

### 2.1 ExecutionRuntime is a shared service

The Execution Runtime primarily composes immutable deployment state and process-owned runtime dependencies. Rebuilding it per request adds no useful isolation and makes resource sharing harder.

The intended lifecycle is:

```text
metis serve
    |
    +-- load deployment
    +-- build semantic runtime
    +-- build BackendRegistry
    +-- build DataSourceRegistry
    +-- build ExecutionRuntime once
    |
    `-- serve N requests through those same objects
          |
          v
       shutdown
          |
          v
    ExecutionRuntime.Close()
```

### 2.2 Request-scoped DriverFactory.Open is insufficient as a production lifecycle

A request flow such as:

```text
ExecuteResolved
    -> resolve secrets
    -> DriverFactory.Open
    -> Executor.Execute
    -> Executor.Close
```

is safe for correctness but ambiguous for expensive live resources.

A naive network-driver implementation could do this per request:

```text
request
    -> sql.Open / create client
    -> authenticate / establish connections
    -> execute query
    -> close pool/client

x 1000
```

That causes repeated authentication, TCP/TLS setup, warehouse-session creation, secret-provider load, lost connection reuse, and connection storms.

The runtime contract therefore needs to distinguish process-scoped DataSource resources from request-scoped execution leases.

### 2.3 Per-query limits do not bound aggregate warehouse pressure

Existing policies such as `query_timeout`, `max_rows`, and `max_bytes` govern one query. They do not prevent hundreds or thousands of individually valid requests from executing concurrently.

Agentic workloads make this particularly important because Agents may naturally fan out across dimensions, segments, comparisons, retries, and analytical subtasks. Metis must not depend on the Agent voluntarily implementing warehouse admission control.

## 3. Goals and non-goals

This RFC MUST:

- define `ExecutionRuntime` as a reusable concurrent process-scoped service;
- prevent connection/client pool construction per query;
- introduce explicit process-scoped DataSource resource ownership;
- preserve request-scoped Executor isolation;
- define deterministic cleanup and shutdown;
- provide explicit per-DataSource execution concurrency limits;
- forbid unbounded execution queues;
- define deterministic overload behavior;
- define thread-safety requirements for runtime extension points;
- avoid external SecretRef resolution on every query when credentials can safely be reused by a live DataSource resource;
- preserve [Core runtime contract](../../specs/operations/runtime-bootstrap.md) Backend/DataSource authority and RFC-0056 compile/execute parity;
- keep Semantic Core independent from connection pooling and warehouse lifecycle;
- support database/sql, SDK-client, remote-warehouse, and embedded-engine backends without forcing one pooling technology.

This RFC does NOT:

- make Execution Runtime mandatory for compile-only deployments;
- expose pool or concurrency knobs to Agents;
- allow Agents to override deployment execution capacity;
- expose arbitrary raw SQL execution;
- move semantic compilation into drivers;
- define distributed admission across multiple Metis processes;
- define tenant scheduling, priority classes, or cluster-wide workload management;
- require one particular Go semaphore or pool library.

## 4. Runtime lifecycle

### 4.1 Process-scoped ExecutionRuntime

For one `metis serve` process, `ExecutionRuntime` is constructed once and reused by concurrent requests.

Request handling MUST NOT construct another `ExecutionRuntime`.

`ExecutionRuntime` MUST be safe for concurrent calls after construction.

### 4.2 Registry freeze

`BackendRegistry` and `DataSourceRegistry` are mutable only while being constructed and validated during bootstrap.

After bootstrap succeeds they are frozen logical read models:

```text
BUILDING
   -> VALIDATED
   -> FROZEN
   -> concurrent reads
```

Runtime registration or mutation is forbidden in v1.

Metis does not add locks merely to make live registry mutation possible. A future hot-reload RFC should use snapshot/replacement semantics rather than mutating maps visible to active requests.

## 5. DataSourceRuntime

### 5.1 Definition

This RFC introduces an internal process-scoped abstraction conceptually equivalent to:

```go
type DataSourceRuntime interface {
    Acquire(context.Context) (Executor, error)
    Close(context.Context) error
}
```

The exact Go shape may differ, but the ownership contract is normative.

A `DataSourceRuntime` represents live reusable execution infrastructure for exactly one configured DataSource instance.

Examples:

```text
doris-prod      -> shared *sql.DB / MySQL-protocol pool
snowflake-prod  -> reusable Snowflake driver/client/session resources
databricks-prod -> reusable SQL warehouse client/transport
duckdb-local    -> shared embedded DB handle with backend-appropriate synchronization
```

`DataSourceRuntime` is not semantic state and MUST NOT depend on `SemanticManifest`, `SemanticGraph`, `SemanticQuery`, `MetricEvaluationPlan`, `SemanticPlan`, `SQLPlan`, or Renderer selection.

### 5.2 Cardinality

Within one Metis process:

```text
DataSource config  1 : 0..1 live DataSourceRuntime
DataSourceRuntime  1 : 0..N request Executors
```

A configured DataSource may remain uninitialized if it is never queried.

### 5.3 Lazy initialization

Bootstrap performs static validation only:

```text
DataSource config
    -> Backend exists
    -> DriverFactory validates config
    -> execution policy valid
```

By default it does not require a successful live warehouse connection.

First execution performs live initialization:

```text
UNINITIALIZED
    -> resolve required SecretRefs
    -> create reusable client/pool
    -> READY
```

This keeps compile-only startup independent from live database availability and avoids creating resources for unused projects.

### 5.4 Concurrent first use

If many requests concurrently hit an uninitialized DataSource, initialization MUST be single-flight/coalesced:

```text
100 requests
    -> one DataSourceRuntime initialization
    -> waiters observe the same success/failure outcome
```

The implementation MUST NOT create 100 independent pools/clients.

A failed initialization MUST NOT permanently poison the process. A later request may retry according to a bounded retry/backoff implementation policy, and concurrent retries for the same DataSource must again coalesce.

## 6. DriverFactory lifecycle contract

The long-term responsibility of a DriverFactory is:

```text
Backend type implementation
    -> validate DataSource config
    -> construct one reusable DataSourceRuntime
```

Conceptually:

```go
type DriverFactory interface {
    DataSourceType() DataSourceType
    ValidateDataSourceConfig(map[string]any) error
    OpenDataSource(context.Context, DataSource, ResolvedSecrets) (DataSourceRuntime, error)
}
```

The exact interface migration may differ, but a DriverFactory MUST NOT create an independently owned network pool for every query.

Typical network-backed ownership becomes:

```text
DriverFactory.OpenDataSource()
    -> DorisDataSourceRuntime
         -> shared *sql.DB

DorisDataSourceRuntime.Acquire()
    -> request-scoped DorisExecutor
         -> borrows shared *sql.DB
```

## 7. Executor ownership

`Executor` remains request-scoped and executes only completed compiler artifacts.

Its lifecycle is:

```text
Acquire
    -> Executor
    -> Execute(CompiledQuery)
    -> ResultStream
    -> close ResultStream
    -> Executor.Close()
```

`Executor.Close()` MUST release only request-owned resources.

For a pooled implementation, closing an Executor MUST NOT close the process-scoped shared pool/client.

Shared pool/client destruction belongs to `DataSourceRuntime.Close()`.

This distinction is required so that one request finishing cannot make the DataSource unavailable to another request.

## 8. Connection and client reuse

For backends whose execution mechanism supports reusable process-safe resources, those resources MUST be DataSource-scoped rather than query-scoped.

The RFC intentionally does not require identical backend tuning because execution technologies differ:

```text
Doris       -> database/sql or protocol connection pool
Snowflake   -> driver/session/client resources
Databricks  -> SQL warehouse client/transport reuse
DuckDB      -> embedded database handle
```

Backend-specific pool tuning MAY remain DriverFactory-owned configuration, for example:

```yaml
doris-prod:
  type: doris
  config:
    address: doris.internal:9030
    database: analytics
    pool:
      max_open_connections: 32
      max_idle_connections: 8
      max_connection_lifetime: 30m
  policy:
    query_timeout: 30s
    max_rows: 100000
    max_bytes: 67108864
    max_concurrency: 24
```

The semantic distinction is:

```text
config.pool.*
    -> DriverFactory-owned transport/resource tuning

policy.max_concurrency / max_queue / queue_timeout
    -> Execution Runtime-owned workload admission policy
```

Pool size and execution concurrency are related operationally but are not the same authority.

## 9. Admission control

### 9.1 Mandatory per-DataSource concurrency ceiling

Every execution-enabled DataSource has a positive `max_concurrency` policy:

```yaml
policy:
  query_timeout: 30s
  max_rows: 100000
  max_bytes: 67108864
  max_concurrency: 32
```

`max_concurrency` is a fail-closed sibling of [Core runtime contract](../../specs/operations/runtime-bootstrap.md)'s `query_timeout`, `max_rows`, and `max_bytes`. An omitted value defaults to `100`; omission MUST NOT mean unlimited concurrency, and zero MUST NOT be interpreted as a sentinel for unlimited execution.

Bootstrap MUST validate the execution bounds through the same DataSource-policy validation path (for example `validateDataSourcePolicy()` or its successor). Zero, negative, malformed, or otherwise invalid `max_concurrency` fails bootstrap before the DataSource becomes executable; Runtime MUST NOT defer this check until the first query.

This ceiling limits simultaneously admitted physical executions for that DataSource.

### 9.2 Admission position

Admission occurs after successful compilation but before acquiring the request execution lease and before issuing the physical query:

```text
SemanticQuery
    -> compile
    -> CompiledQuery
    -> acquire DataSource capacity
    -> ensure DataSourceRuntime READY
    -> Acquire Executor
    -> execute
```

Compile CPU admission is outside this RFC.

### 9.3 Bounded queueing

Infinite waiting is forbidden.

Policy MAY additionally declare:

```yaml
policy:
  max_concurrency: 32
  max_queue: 64
  queue_timeout: 2s
```

Semantics:

- `max_concurrency`: maximum active admitted executions;
- `max_queue`: maximum requests waiting for execution capacity;
- `queue_timeout`: maximum time a queued request may wait.

The recommended default is `max_queue: 0`, meaning fail fast when execution capacity is exhausted.

If `max_queue > 0`, `queue_timeout` MUST be finite and positive.

Agent-facing requests MUST NOT provide or relax these values.

Higher-level Analytics Runtime operations MUST use the same admission boundary.

## 10. Capacity errors

Capacity exhaustion is distinct from query execution timeout.

Execution Runtime requires a typed internal outcome such as:

```text
EXECUTION_CAPACITY_EXCEEDED
```

The application-facing projection may expose a stable code such as:

```text
QUERY_EXECUTION_BUSY
```

REST SHOULD map current-capacity exhaustion to HTTP 429. MCP should expose the corresponding typed application error rather than a generic internal failure.

A capacity rejection MUST occur before opening/acquiring a request Executor and before issuing a physical query.

It MUST NOT return a partial result.

## 11. Secret lifecycle

Secret resolution is not inherently per-query.

A process handling 1000 requests MUST NOT automatically produce 1000 calls to AWS Secrets Manager, Vault, Azure Key Vault, GCP Secret Manager, or another provider solely because request execution is frequent.

Secrets needed to construct a long-lived DataSource client/pool SHOULD normally be resolved as part of DataSourceRuntime initialization:

```text
first execution
    -> resolve SecretRefs
    -> initialize DataSourceRuntime
    -> reuse live resource for later requests
```

This does not weaken [Core runtime contract](../../specs/operations/runtime-bootstrap.md) secret safety rules.

A runtime MUST NOT silently use stale credentials forever if refresh fails. Backends with expiring/rotating credentials must either support explicit safe refresh or invalidate and recreate the affected DataSourceRuntime.

Resolved secret values MUST NOT escape through logs, metrics, traces, compiler artifacts, errors, ResultSets, or observable cache keys.

## 12. Concurrency-safety contracts

The following contracts are normative:

### ExecutionRuntime

Concurrent `ResolveDataSource`, `Execute`, and `ExecuteResolved` calls are supported after construction. Request-specific mutable state remains request-local.

### BackendRegistry and DataSourceRegistry

Concurrent reads are supported after bootstrap freeze. Runtime writes are forbidden.

Returned DataSource values must not expose mutable internal registry state.

### Renderer

A Backend Renderer may be shared by concurrent compilations and MUST therefore be immutable or otherwise concurrency-safe. Request-specific rendering state must not be stored on the shared Renderer instance.

### DriverFactory

DriverFactory implementations may be invoked concurrently for different DataSources and MUST be concurrency-safe.

Same-DataSource duplicate live initialization is coalesced by Execution Runtime.

### DataSourceRuntime

`Acquire` is safe for concurrent requests.

### SecretResolver

SecretResolver may be called concurrently for different DataSources and MUST be concurrency-safe.

### ExecutionObserver

Observers MUST tolerate concurrent completion events and MUST NOT serialize all execution behind a global lock.

## 13. Conceptual ExecutionRuntime model

A possible internal implementation is:

```go
type ExecutionRuntime struct {
    sources  *DataSourceRegistry
    backends *BackendRegistry
    secrets  SecretResolver
    observer ExecutionObserver

    entries map[string]*dataSourceEntry
    state   runtimeState
}

type dataSourceEntry struct {
    source  DataSource
    backend Backend

    admission *AdmissionGate

    // lazily initialized process-scoped live resource
    runtime DataSourceRuntime

    // lifecycle synchronization protects initialization/state transitions only
    lifecycle ...
}
```

Lifecycle synchronization MUST NOT wrap the duration of query execution.

Bad:

```text
lock datasource
    -> execute a 30-second query
unlock datasource
```

Correct:

```text
brief lifecycle synchronization
    -> obtain READY DataSourceRuntime
    -> release lifecycle synchronization
    -> execute concurrently under admission control
```

## 14. End-to-end request flow

The resulting public execution flow is:

```text
QueryMetricsService
    -> resolve Project
    -> resolve DataSource
    -> resolve Backend
    -> compile with Backend.Renderer
    -> CompiledQuery
    -> ExecutionRuntime.ExecuteResolved
         -> acquire DataSource execution capacity
         -> ensure DataSourceRuntime READY
              -> lazy SecretRef resolution if needed
              -> initialize shared client/pool once
         -> DataSourceRuntime.Acquire
         -> request Executor
         -> Executor.Execute(CompiledQuery)
         -> ResultStream
         -> bounded normalization
         -> ResultSet
         -> Executor.Close
         -> release execution capacity
```

Compile and execution remain separate stages. Neither stage becomes responsible for the other's internal pipeline. The application-layer service remains the workflow orchestrator.

## 15. Graceful shutdown

`metis serve` shutdown MUST eventually invoke a runtime lifecycle operation conceptually equivalent to:

```go
func (r *ExecutionRuntime) Close(ctx context.Context) error
```

Recommended lifecycle:

```text
RUNNING
   -> DRAINING
        - reject new execution admission
        - allow bounded in-flight work
   -> CLOSING
        - cancel remaining work after grace deadline
        - close initialized DataSourceRuntime resources
   -> CLOSED
```

Each DataSourceRuntime MUST:

- close its process-scoped pool/client exactly once;
- honor `Close(ctx)` cancellation/deadline or otherwise provide bounded close semantics;
- reject new acquisitions after close;
- tolerate repeated shutdown attempts safely;
- not leak secrets in cleanup errors.

Shutdown is globally phased across DataSources: stop admission for all entries,
wait for all active work against the same shutdown context, cancel all remaining
leases when that context expires, then initiate bounded resource cleanup for all
entries. It MUST NOT drain and close DataSources serially, and MUST NOT replace
an expired shutdown context with `context.Background()`. Executors still active
when the shutdown deadline expires must be cancelled through their request
contexts before shared resources are destroyed. A Driver implementation that
does not honor `Close(ctx)` must still guarantee that its close operation is
bounded; Runtime also bounds how long it waits for driver cleanup.

## 16. Failure isolation

Runtime state is per DataSource.

A failure initializing or executing against one DataSource MUST NOT invalidate unrelated DataSources.

Example:

```text
doris-prod initialization fails
    -> finance query unavailable

snowflake-prod remains READY
    -> commerce query succeeds
```

Process-wide failure is reserved for broken global invariants rather than one warehouse being unavailable.

## 17. Observability

Execution observability SHOULD expose bounded operational signals such as:

```text
execution_inflight
execution_queue_depth
execution_capacity_rejected_total
execution_datasource_initialization_total
execution_datasource_initialization_duration
execution_datasource_runtime_ready
execution_datasource_close_total
```

Labels must remain bounded and consistent with the existing observability contract.

Raw SQL, semantic request text, credentials, SecretRefs, and endpoint strings MUST NOT become metric labels.

Driver-specific pool statistics MAY additionally expose bounded metrics such as open, idle, and in-use connections where supported.

## 18. Compile-only and Analytics Runtime behavior

Compile-only deployments remain unaffected. Without execution-enabled DataSources, Metis does not initialize database pools, live clients, credentials, or warehouse sessions.

Higher-level deterministic analytical operations such as attribution MUST reuse this same Execution Runtime and DataSource admission boundary. Analytics Runtime MUST NOT create a separate warehouse pool or bypass concurrency policy.

If one analytical operation produces multiple independent physical queries, each execution consumes normal DataSource capacity. Group scheduling/reservations are future scope.

## 19. Alternatives rejected

### ExecutionRuntime per request

Rejected. It repeats immutable state and makes process-level resource reuse harder without providing useful isolation.

### Executor-owned connection pool

Rejected. It makes request cleanup destroy reusable transport state and encourages connection storms.

### Put one generic pool directly on ExecutionRuntime

Rejected as a universal abstraction. Backends may use `database/sql`, SDK clients, sessions, transports, or embedded handles. Backend-owned `DataSourceRuntime` is the correct polymorphic resource boundary.

### Global process semaphore only

Rejected. Different DataSources have independent warehouses and capacities. Per-DataSource admission is the primary authority. A future global ceiling may be added independently.

### Unlimited queue

Rejected. It converts overload into unbounded memory usage and unpredictable latency.

### Use connection-pool max-open as the only admission control

Rejected. Pool limits govern transport resources, not Metis workload semantics, and some backends do not expose a conventional pool.

### Let Agents set concurrency

Rejected. Warehouse capacity is deployment policy, not semantic intent.

### Resolve secrets on every query

Rejected as the default lifecycle because it couples query QPS directly to secret-provider load and latency.

## 20. Rollout and migration

Implementation SHOULD proceed in phases.

### Phase A: lifecycle contracts

Introduce `DataSourceRuntime`, `ExecutionRuntime.Close`, registry freeze semantics, and explicit concurrency-safety documentation.

### Phase B: DriverFactory migration

Move DriverFactory from request-owned physical-resource construction toward DataSourceRuntime construction. Existing fake/test drivers adopt the same lifecycle contract.

### Phase C: real shared resource implementation

Implement at least one real backend proving that repeated queries reuse one DataSourceRuntime and one underlying pool/client.

### Phase D: admission control

Add `max_concurrency`, `max_queue`, `queue_timeout`, typed capacity errors, and REST/MCP projection.

### Phase E: graceful shutdown

Wire server shutdown to `ExecutionRuntime.Close` using the existing shutdown deadline.

### Phase F: race and load gate

Require concurrent execution tests, race-detector coverage, and a 1000-request bounded-resource scenario.

## 21. Test and acceptance criteria

This RFC cannot become `Implemented` until all of the following are demonstrated.

### 21.1 Sequential reuse

For one DataSource and 1000 sequential executions:

```text
ExecutionRuntime creation count       = 1
DataSourceRuntime creation count      = 1
shared pool/client creation count     = 1
successful query execution count      = 1000
```

### 21.2 Concurrent initialization

For at least 100 concurrent first requests to one uninitialized DataSource,
DataSourceRuntime initialization count is exactly one whether that attempt
succeeds or fails. Every waiter in one attempt generation receives the same
result. A request arriving after a failed generation completes may create
exactly one new retry generation; waiters from the failed generation MUST NOT
turn into serialized initialization retries.

### 21.3 Concurrency ceiling

With `max_concurrency: 8` and at least 100 concurrent attempts, observed active physical executions never exceed 8.

### 21.4 Queue bound

With `max_concurrency: 8` and `max_queue: 16`, runtime state never exceeds 8 active plus 16 queued executions for that DataSource. Additional requests fail with the typed capacity error.

### 21.5 Fail-fast mode

With `max_queue: 0`, requests beyond active capacity fail without acquiring/opening an Executor or issuing a physical query.

### 21.6 Executor ownership

Closing one request Executor does not close or invalidate the shared DataSourceRuntime and does not prevent later requests from executing.

### 21.7 Shutdown

Tests prove that:

- new execution stops being admitted once draining starts;
- in-flight work receives bounded drain time;
- remaining work is cancelled at the shutdown deadline;
- all DataSources enter each stop/drain/cancel/cleanup phase together rather
  than consuming the deadline through serial per-DataSource shutdown;
- cleanup never substitutes an unbounded context after the deadline expires;
- each initialized DataSourceRuntime closes exactly once;
- repeated `Close` calls are safe;
- no physical query starts after runtime reaches `CLOSED`.

### 21.8 DataSource isolation

Failure, overload, or initialization failure for one DataSource does not block unrelated DataSources.

### 21.9 Secret behavior

For static credentials and 1000 successful requests through one initialized DataSourceRuntime, external SecretRef resolution does not occur once per query. Secret values do not escape through errors or observability.

### 21.10 Race safety

The affected execution/bootstrap/application packages pass race-detector tests, including dedicated concurrent tests for:

```text
ResolveDataSource
ExecuteResolved
DataSourceRuntime initialization
Acquire
execution completion observation
Close
```

### 21.11 Load evidence

At least one 1000-request test reports:

```text
requests
successes
capacity rejections
max observed concurrency
DataSourceRuntime initialization count
pool/client creation count
latency distribution
race result
```

Acceptance is based on correctness and bounded resource behavior, not a fixed QPS target.

## 22. Documentation updates

Implementation must update:

- `docs/proposals/README.md`;
- [Core runtime contract](../../specs/operations/runtime-bootstrap.md) references to DriverFactory/Executor lifecycle where needed;
- RFC-0056 runtime execution flow;
- Execution Runtime design documentation;
- DataSource configuration specification;
- REST/MCP execution-capacity error contract;
- backend authoring documentation;
- production deployment examples;
- observability documentation.

Backend documentation must explicitly distinguish:

```text
DriverFactory
    type-level implementation factory

DataSourceRuntime
    process-scoped concrete DataSource resource

Executor
    request-scoped execution lease
```

## 23. Durable architecture

With this RFC, runtime ownership is:

```text
                    Application Orchestration
                           |
                           v
                  QueryMetricsService
                     /           \
                    /             \
                   v               v
          CompileService      ExecutionRuntime
                |                  |
                |                  v
                |           DataSourceRuntime
                |                  |
                |           shared pool/client
                |                  |
                v                  v
          CompiledQuery ------> Executor
                                   |
                                   v
                                Warehouse
                                   |
                                   v
                             ResultStream
                                   |
                                   v
                              ResultSet
```

Authority and ownership remain intentionally separate:

```text
Semantic Core
    owns semantic truth and physical compilation

ExecutionRuntime
    owns workload admission and execution lifecycle

DataSourceRuntime
    owns reusable concrete DataSource resources

Executor
    owns one request execution lease

Application Orchestrator
    composes compile and execute

Agent
    supplies semantic intent only
```

The resulting operational rule is:

> **Initialize runtime infrastructure once, share expensive DataSource resources safely, isolate query-local state per request, and bound aggregate warehouse pressure before execution begins.**
