# Runtime Bootstrap

This specification defines the current contract for loading project semantics
and optional execution configuration. List-shaped project manifests and
`ExecutionBinding` configuration are not accepted.

## Root deployment manifest

`metis serve --config <path>` loads one thin root `metis.yaml`:

```yaml
version: 1
default_project: finance

projects:
  finance:
    path: ./projects/finance.yaml
    data_sources: [orders, customers]
  demo:
    path: ./projects/demo.yaml

data_sources:
  path: ./datasources.yaml
```

`projects` is a non-empty map. Each map key is the deployment-scoped Project
Key/Name. `default_project`, when present, MUST reference an existing key.
Configuration decoding is strict: unknown fields fail startup.

Project registration owns deployment wiring only. It references one semantic
project manifest and MAY apply one or more named DataSources. The singular
`data_source` field remains a shorthand for a one-item set; a registration MUST
NOT specify both forms. There is no
`ExecutionBinding`, `target`, `default_target`, or `execution.projects` route
map in the accepted grammar.

When exactly one DataSource is applied, every semantic model without explicit
placement uses it. When several are applied, every semantic model MUST select
one applied name through this Ossie extension:

```yaml
semantic_model:
  - name: orders
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"data_source","name":"orders"}'
```

The name is a stable Project-level logical selector. Endpoint, credential,
database-family, policy, and Driver configuration remain exclusively in the
deployment DataSource registry. The extension binds the complete semantic
model, so one compiled query always uses one DataSource and one Renderer.
Cross-source joins, request-time source selection, and federation are not part
of this contract.

## Semantic project manifests

A semantic project manifest contains one or more `semantic_sources` and does
not repeat Metis Project Key/ProjectID. Source paths are resolved relative to
the project manifest and MAY be filesystem globs. Every pattern MUST match;
matches are sorted and loaded through the canonical Ossie loader.

All loaded Ossie documents contribute to one deterministic project-scoped
SemanticManifest. Document boundaries do not create semantic namespaces or
precedence. Duplicate canonical semantic identities fail closed.

The manifest MAY also contain a `quality` policy with a `severities` map from
registered `MODEL_QUALITY_*` codes to `error`, `warning`, or `info`, plus a
`publication_threshold` using the same values. This policy controls offline
quality severity and eligibility reporting only. It MUST NOT change semantic
assembly, runtime loading, resolution, planning, or execution. Unknown fields,
unknown diagnostic codes, and invalid severity values fail deterministically.

Runtime bootstrap and offline validation use `app/service/source.LoadProject`.
Source inspection does not activate a running generation. Embedders explicitly
use the [runtime replacement contract](semantic-runtime-activation.md).

## Project resolution

Every application entrypoint uses one resolution function:

```text
explicit project
    > configured default_project
    > sole registered project
    > PROJECT_REQUIRED
```

An invalid explicit project MUST fail and MUST NOT fall back. An invalid
configured default is a bootstrap error. The sole-project rule applies only
when neither explicit nor configured default selection exists.

The resolved stable Project identity is then passed to the shared
`ProjectAuthorizer` before any semantic inventory, DataSource, Backend, or
source state is accessed. The default network runtime uses the Principal scope
adapter and fails closed; trusted offline runtimes must explicitly select the
local all-access adapter. See
[`project-action-authorization.md`](project-action-authorization.md).

Runtime assembly accepts an independent `WithAssetVisibilityPolicy` adapter.
When omitted, discovery installs the explicit all-visible compatibility policy;
when supplied, the same instance governs discovery, compile, and execution
through the shared DiscoveryService. It is evaluated only after project
authorization as specified by
[`../semantic/asset-governance.md`](../semantic/asset-governance.md).

## DataSource registry

Embedders MAY configure `WithDataAccessPolicy` for shared compilation and
execution services. The adapter is retained across semantic-generation rebuilds.
The default is the explicit no-restriction compatibility adapter; nil fails
closed. This is separate from DataSource execution safety policy and introduces
no manifest policy schema. See [data access policy](data-access-policy.md).

Root `metis.yaml` MAY reference exactly one separate DataSource registry file.
Each named DataSource contains one `type`, type-owned instance configuration,
external-value references, and bounded execution policy. Project manifests and
Ossie assets MUST NOT contain endpoint, credential, driver, Renderer, or
execution-policy configuration.

`execution/backend.BackendRegistry` is the separate process-owned type registry.
Each registered type binds the same Renderer instance that will compile runtime
work and one `execution/driver.Factory`; SQLDialect derives only from that exact
Renderer. Registration rejects missing,
duplicate, or incompatible bindings. Registering a compile-only Renderer in
`renderer.Registry` MUST NOT implicitly create a Backend; a dialect may be
available for compile-only use without an executable DataSource type.

`execution/datasource.DataSourceRegistry` loads that one file with strict record fields.
It retains named `type`, driver-owned `config`, and bounded `policy` values.
Every execution-enabled DataSource MUST configure positive `query_timeout`,
`max_rows`, and `max_bytes` deployment ceilings. `max_concurrency` defaults to
the bounded value `100` and, when configured, MUST be positive. This default is
an intentional deployment policy: omission is bounded, not unlimited, but
operators SHOULD set an explicit warehouse-appropriate value because safe
parallelism differs substantially across remote warehouses, small clusters,
and embedded engines.
`max_queue` defaults to fail-fast when absent or zero; a positive queue also
requires a finite positive `queue_timeout`. The registry file MUST contain
exactly one YAML document. The config is validated by the matching DriverFactory's
`ValidateConfig` method. `config` is exactly one flat `map[string]string`.
Two closed reference forms are accepted:

- `${ENV_NAME}` is shorthand for `SecretRef{Provider: "env", Key: "ENV_NAME"}`;
- `secret://<provider>/<key>` is a provider-neutral locator, for example
  `secret://aws-secrets-manager/prod/metis/doris-password`.

Provider names use lowercase letters, digits, underscores, or hyphens and keys
are non-empty whitespace-free strings. Interpolation, filters, partial
substitution, lowercase environment names, and nested objects are not part of
the grammar. Sensitive fields MUST contain a reference and MUST NOT contain
plaintext. Bootstrap receives the process-owned BackendRegistry explicitly,
validates every source against it, and keeps no derived project-to-source route
map.

For example:

```yaml
doris-prod:
  type: doris
  config:
    host: "${METIS_DORIS_HOST}"
    port: "${METIS_DORIS_PORT}"
    database: analytics
    username: metis_reader
    password: "${METIS_DORIS_PASSWORD}"
  policy:
    query_timeout: 30s
    max_rows: 100000
    max_bytes: 67108864
    max_concurrency: 8
```

At DataSource registration, Core copies the flat map and preserves every
reference string. It does not read the environment. Nested containers, typed
values, and template expressions are rejected by the strict YAML shape or
reference parser. This keeps deployment configuration comparable across Doris,
ClickHouse, DuckDB, and future Backends without defining a parallel profile
schema or database-specific authentication hierarchy in Core.

Drivers receive only `execution/driver.OpenRequest{Config, Secrets}` when a
process-scoped resource is opened. `Config` is a Core-owned flat copy.
Every reference, including `host` and `port`, remains unchanged in that copy.
Resolved values are exposed only through exact
`Secrets.Value(SecretRef)` lookup; the Driver may copy a value into a private
connection object but MUST NOT write it into Config. Sensitive plaintext never
enters either the stored DataSource Config or Driver Config. Driver code does
not receive a DataSource identity or its timeout, row, byte, concurrency, or
queue policy.

Resolved values are scoped to one lazy-open attempt. Runner does not retain
them in DataSourceRegistry, `dataSourceEntry`, or the opened Runtime record after
DriverFactory returns. A Driver client or pool may necessarily retain private
authentication state according to its database SDK, but that state is neither
Config nor a Metis persistence surface.

Bootstrap validates all project/DataSource references and every DataSource type
against BackendRegistry. Duplicate identities, unknown types, missing Backends
or DriverFactories, invalid type-owned schemas, and plaintext values in
secret-annotated fields fail closed.

The default `metis` binary and production container remain CGO-free and
explicitly compose the executable `clickhouse` and `doris` Backends. The opt-in
DuckDB build flavor, selected with `CGO_ENABLED=1 -tags duckdb`, explicitly
composes `clickhouse`, `doris`, and `duckdb`. A default binary rejects DuckDB DataSources during
bootstrap rather than advertising an unusable Backend; selecting the `duckdb`
tag without CGO is a build error.

The ClickHouse DriverFactory uses the official Go client over HTTP or HTTPS and
accepts the type-owned config fields `scheme` and `address` (required),
`database`, `username`, and an optional `password` external-value reference.
The Doris DriverFactory uses Doris's
MySQL-compatible protocol and accepts the
type-owned config fields `host` and `port` (required), `database`, `username`,
and an optional `password` external-value reference. The DuckDB DriverFactory uses
the official embedded Go client, accepts exactly one required `path`, and opens
that file-backed database with `access_mode=read_only`; DSN query components and
fragments, along with arbitrary settings, are rejected. Default containers and
cross-platform release archives remain CGO-free and execute ClickHouse and Doris.

The production process passes this BackendRegistry and the `env` reference
resolver into bootstrap; a configured DataSource is therefore validated and
wired during the same `metis serve` assembly path that exposes `query_metrics`
and `get_dimension_values`.
The official process resolves only `env`. A deployment may inject a
provider-aware `SecretResolver` for AWS Secrets Manager or another vault without
changing Config shape, Runner, or a Driver; an unconfigured provider fails
closed and never falls back to plaintext. Compile-only SQL dialects do not
become runtime Backends merely because their Renderers are registered.

A project without `data_source` or `data_sources` is valid for compile-only use.
Runtime execution for that project fails explicitly. An explicit semantic-model
placement may remain in a compile-only release; it acquires runtime authority
only when the Deployment applies that named DataSource. Agent Project discovery
reports `compile_sql` for a compilable Project and additionally reports
`query_metrics` only when an applied DataSource and Backend resolve through
Runner. This inspection performs no connection or secret-resolution work.

## Governed execution boundary

`execution/runner.Runner` is reachable only through governed `query_metrics`
and the closed `attribute_metric`, `compare_metrics`, and
`get_dimension_values` service operations. It resolves the named DataSource's Backend,
then acquires that DataSource's bounded execution capacity before lazily
resolving SecretRefs and opening its one process-scoped `DataSourceRuntime`.
The DataSourceRuntime owns a reusable pool/client; it lends an Executor only a
request-scoped lease and is closed during server shutdown. Runtime passes an
Executor only a private snapshot of `CompiledQuery`
(`SqlRenderResult + OutputSchema`). Runtime validation and result accounting use
a separate content-identical snapshot, so mutation within an Executor cannot
alter Runtime validation evidence, the caller artifact, or the returned schema.
Executors cannot depend on semantic manifests, resolver/planner state, SQL
plans, Renderers, or route selection.

Concurrent lazy initialization is attempt-scoped. All callers waiting on one
initialization generation receive that generation's success or failure; they do
not retry serially after a shared failure. Only a request arriving after the
failed generation completes may start the next attempt. Shutdown stops
admission for all DataSources before waiting for active work across all of them.
At the shutdown deadline Runtime cancels all remaining request leases and
initiates bounded cleanup for all initialized resources without replacing the
expired context with an unbounded one. Every `execution/driver.Runtime` implementation
MUST honor its `Close(ctx)` cancellation/deadline or otherwise guarantee bounded
close behavior.

Admission is FIFO within each DataSource. A newly arriving request cannot take
a permit ahead of an older queued request, including when acquisition races
with a release. Cancellation and queue timeout remove exactly that waiter and
cannot leak or manufacture a permit. `max_queue=0` remains immediate rejection;
bounded queueing requires both `max_queue` and `queue_timeout`.

One effective execution deadline covers lazy DriverFactory open, Runtime
acquisition, query execution, and every ResultStream read. Context cancellation
before any of those boundaries, while the engine is running, or while rows are
being read fails closed. Runner closes every returned stream and Executor on
decode failure, partial-stream failure, cancellation, or limit failure. It does
not automatically retry physical queries: a Driver error does not prove that a
warehouse performed no work or that the remaining deadline can afford another
attempt. After a fault, the process-scoped Runtime remains reusable when its
Driver can safely reuse it; shutdown and explicit recycle are the only Core
lifecycle authorities.

`query_metrics` resolves a project and semantic model once, resolves the
model's DataSource placement once, resolves that DataSource's Backend once, and
uses that exact Backend Renderer for semantic resolution, planning, rendering,
and execution. A sole applied source is inferred; a multi-source Project must
have complete explicit model placement before the generation can activate.
It accepts semantic intent only and returns `query_id`, normalized schema,
rows, and count. Compile-only deployments still register the public REST/MCP
operation but return `QUERY_EXECUTION_UNAVAILABLE`; they never infer a target.

`get_dimension_values` follows the same exact
Project-to-model-to-DataSource-to-Backend Renderer authority and
execution-unavailable behavior. It compiles and executes
exactly one normalized semantic query with a caller row ceiling of `limit + 1`,
then validates and projects one typed dimension column. It accepts no runtime
placement override and returns no SQL.

This is an ownership contract, not general-purpose deep copying. Compilation
constructs an artifact with owned parameter and schema containers; after the
artifact is published, callers MUST treat it and its nested values as immutable.
Runtime copies those known containers at its boundary and gives the Executor a
separate copy. `QueryParameter.Value` has a closed domain of nil, strings,
booleans, built-in integer types, finite built-in floating-point types,
`json.Number`, and `[]byte`; only `[]byte` requires an element copy. Unknown
pointers, structs, slices, maps, and other mutable values fail before a
DriverFactory is opened. Runtime does not use reflection or attempt to infer
copy semantics for arbitrary objects.

The runtime applies the strictest non-zero caller and deployment timeout before
secret resolution and keeps it active while opening and consuming a
ResultStream. Row-count and byte-count ceilings apply while consuming results.
The selected Executor reads database result metadata and decodes physical wire
values; Runtime then normalizes every non-NULL value according to the logical
`OutputSchema`. Agent-facing JSON uses strings for `String`, exact decimal text
for `Decimal` (including Doris DECIMALV3 and ClickHouse Decimal64), JSON numbers
for `Integer` and `Float`, booleans for `Boolean`, and strings for temporal
types. Temporal strings are parsed against their declared Ossie datatype and
returned in canonical date, time, naive-datetime, or RFC3339-with-offset form;
a timestamp-shaped string cannot satisfy a declared `Date`. An omitted
datatype remains unspecified and is not guessed from a wire value. A value that
cannot be represented losslessly under its declared logical
datatype fails with stable `QUERY_RESULT_SCHEMA_MISMATCH`; in particular, a
Decimal decoded through a binary float is rejected rather than returning
silently rounded data. Its value-free details contain
`stage=result_normalization`, the semantic output `column`,
`expected_datatype`, a closed `observed_family`, and `retryable=false`. The
offending value, concrete Driver type, and underlying error are never exposed.
It closes every non-nil stream and Executor returned by a driver, including
when the same call also returns an error. Cancellation, unaccountable results,
limit breaches, and cleanup failures return a redacted `ExecutionError` and
never a partial successful ResultSet. Result rows contain only normalized
scalars or explicitly supported owned containers (`[]byte`, `[]any`,
`[]string`, `[]int`, `map[string]any`, and `map[string]string`, recursively
subject to the same closed value domain). Runtime copies every supported
mutable container. Unknown driver objects fail instead of escaping through
`ResultSet`. Internal observations contain only the opaque query ID, DataSource
type, stable execution code, duration, and aggregate row/byte counts; observer
failure does not alter execution behavior.

## Runtime assembly

Each project is validated and indexed independently. Bootstrap MUST:

- preserve project namespaces while producing a deterministic aggregate
  SemanticManifest digest;
- assemble one deterministic Project Registry and optional DataSourceRegistry;
- keep BackendRegistry type-level and free from instance configuration;
- expose shared discovery, compilation, and project resolution to REST and MCP;
- start compiler-only with no DataSource registry, credentials, or execution
  implementation.

`query_metrics` remains the only generic Agent-facing execution capability.
`attribute_metric` is a closed multi-query analytical workflow with sequential,
bundle-wide budgets and all-or-nothing evaluation. `compare_metrics` is the
second closed analytical workflow. It compiles two ordinary period queries with
one exact Backend Renderer, executes them sequentially under one timeout and
cumulative row/byte budgets, and returns only after deterministic tuple
alignment succeeds. `get_dimension_values` is a closed single-query live-value
discovery workflow with bounded typed projection and truncation evidence.
Registering any of these services does not imply
connectivity: without an executable DataSource and Backend it returns
`QUERY_EXECUTION_UNAVAILABLE`.

## Readiness

`GET /readyz` returns a sorted `projects` array. It does not expose a generation
or aggregate digest because Projects activate independently and no
deployment-wide semantic version exists. Project selection convenience does
not merge namespaces.
