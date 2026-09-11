# Runtime Bootstrap Architecture

The accepted bootstrap architecture separates semantic project registration,
concrete database instances, and type-level implementations:

```text
root metis.yaml
  |
  +-- projects[key]
  |     +-- semantic project manifest -> Ossie sources
  |     `-- optional data_source reference
  |
  +-- optional default_project
  |
  `-- one optional datasources.yaml
             |
             v
       DataSourceRegistry[name]
             |
             v
       DataSource{type, instance config}
             |
             v
       BackendRegistry[type]
          /             \
         v               v
     Renderer       DriverFactory
```

Project resolution happens once through explicit project, configured default,
sole registered project, or `PROJECT_REQUIRED`. After resolution, the root
Project Registration supplies its optional DataSource reference directly; no
ExecutionBinding or parallel route map intervenes.

Before semantic or runtime state is read, the shared application service asks
one `ProjectAuthorizer` for the authenticated Principal, resolved Project, and
closed action. REST and MCP do not own policy. Network runtime assembly uses
the Principal scope adapter; trusted offline compositions opt into all-access
explicitly. Capability advertisement intersects runtime wiring with the same
action decision.

Semantic project manifests are identity-free packaging manifests containing
one or more semantic-source paths. Each project builds one isolated
SemanticManifest contribution. Runtime composition never merges project
namespaces, and filename or load order never resolves semantic conflicts.
Bootstrap deterministically rebuilds one immutable, manifest-scoped
`SemanticGraph` from the assembled manifest digest. The graph holds only
existing relationship and metric-dependency topology; it is not a second
semantic authority or a query-scoped `SemanticPlan` wrapper.

Bootstrap and offline source tools share `app/service/source.LoadProject`.
Each Project is assembled into an immutable generation. The embedding
`Manager.Replace` API builds a complete replacement before swapping that
Project's pointer; request scopes keep using their captured generation.
Runner and connection resources remain process-owned. See the
[runtime generation specification](../../specs/operations/semantic-runtime-activation.md).

Compile-only selects one Renderer by explicit SQLDialect. The `cmd/metis`
composition root explicitly links concrete executable Backends; application
and bootstrap packages receive the resulting BackendRegistry rather than
selecting a warehouse package. Runtime obtains the Renderer and DriverFactory
together from the Backend selected by `DataSource.type`. BackendRegistry owns
database-family implementation;
DataSourceRegistry owns concrete instance configuration. Neither substitutes
for the other.

Concrete DataSource configuration is a flat `map[string]string`, shaped like
the `config` section of `datasources.yaml` rather than an external tool's
profile hierarchy. `${ENV_NAME}` is a reference token, not template language.
`secret://<provider>/<key>` is the provider-neutral form for an injected vault
resolver. The registry preserves tokens without reading the environment or
contacting a vault. Runner resolves them lazily into an exact-reference value
capability while every Config copy retains the token. Drivers move individual
values only into private connection objects through `driver.Secrets` lookup.
The official composition supplies `env`; deployments can inject AWS Secrets
Manager or another provider without changing DataSource shape. Resolved values
are local to lazy initialization and are not retained by Runner after
DriverFactory returns. This lets Backends add their own flat connection keys
without making Core own warehouse authentication models or allowing credentials
into DataSource Config.

The default CGO-free composition includes ClickHouse and Doris. An explicit
`CGO_ENABLED=1 -tags duckdb` build flavor adds DuckDB; merely enabling CGO does
not change the binary's Backend inventory. DuckDB owns one read-only embedded
database handle per concrete DataSource and lends request-scoped Executors from
it. This remains ordinary Backend composition: Runner has no DuckDB branch,
and the DuckDB Renderer is the same instance used for runtime compilation.
Default containers and cross-platform archives remain CGO-free and fail closed
if configured with a DuckDB DataSource. ClickHouse uses its official Go Driver
over HTTP(S), while Doris uses its MySQL-compatible protocol.

REST and MCP receive the same aggregate discovery/compile services, Project
Resolver, `QueryMetricsService`, `AttributeMetricService`, and
`CompareMetricsService`. `query_metrics` is the only generic public execution
capability; attribution and comparison are closed analytical verticals. All
three compile through the Backend-selected Renderer and pass completed
`CompiledQuery` values atomically to
`execution/runner.Runner`. A
compile-only deployment keeps the same public surface but returns the stable
execution-unavailable error; it does not guess an execution target.

Comparison uses two ordinary semantic queries and keeps tuple alignment and
exact change arithmetic in database-independent `analytics/comparison`.
Attribution and comparison share Runtime contracts, not a generalized
analytical plan or evaluator hierarchy.

`execution/runner.Runner` is process-scoped. It freezes the completed registries and
lazily creates at most one `DataSourceRuntime` for each executable DataSource.
That resource owns a reusable pool/client, while each query borrows a
request-scoped Executor lease. Per-DataSource admission occurs after
compilation and before resource acquisition; `max_concurrency` defaults to
`100` and can be explicitly tightened or raised, with bounded queueing opt-in
through `max_queue` and `queue_timeout`. Server
shutdown stops new admission, drains active work within its deadline, and then
closes initialized DataSource resources.

Each DataSource queue transfers permits in FIFO order. Queue cancellation and
timeout remove only their own waiter, so a fault cannot leak capacity or allow a
later request to overtake surviving work. The Runner's effective deadline
continues through Driver open, Executor acquisition and execution, and row
consumption. It owns complete-result atomicity and resource cleanup across all
of those stages. Driver errors are redacted and never trigger an inferred
retry; successful reuse after a fault is proved by the backend resilience
contract rather than assumed from a vendor client.

## Transition

`app/bootstrap` now loads only map-shaped root Project Registrations and
identity-free semantic project manifests. `execution.Resolver` and
`ExecutionBinding` configuration have been removed. CompileTarget and semantic
Engine authority are likewise absent. Current runtime behavior is specified by
the runtime-bootstrap and Agent query contracts rather than a migration plan.
