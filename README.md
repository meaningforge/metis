# Metis — The Unified Semantic Runtime for Agentic Analytics

Metis is an open-source, Ossie-first, engine-neutral semantic layer runtime engine.
It sits between AI agents or applications and analytical databases, turning
structured requests for metrics and dimensions into deterministic SQL and,
when execution is configured, analytical results.

**Agents reason. Metis resolves semantics. Engines execute.**

For example, an agent asks for **total revenue by region**. Metis resolves the
metric definition and compatible dimension from an Ossie model, plans the
required joins and aggregations, and generates SQL for the selected database.
The same semantic definitions serve the CLI, MCP, REST, and embedded Go APIs.

- **Ossie-first:** Apache Ossie models define metrics, dimensions, relationships,
  and ontology concepts that Metis discovers, validates, and resolves.
- **Engine-neutral:** semantic resolution and planning are shared across database
  targets. Renderers generate dialect-specific SQL; execution backends manage
  database access. Built-in targets are Doris, ClickHouse, and DuckDB.
- **Deterministic:** structured semantic requests go through validation, resolution,
  planning, and compilation. Metric definitions and relationship rules come from
  the model.
- **Ready for analytical workflows:** discover semantic assets, compile SQL, query
  metrics, compare periods, and analyze metric-change attribution.

```text
Agent or application
        |
        | metrics, dimensions, filters, time ranges
        v
      Metis <----- Apache Ossie models
        |
        +--- discover / validate / resolve / plan
        |
        +--- compile SQL ---> database client of your choice
        |
        +--- execute through a configured backend ---> analytical results
```

Compilation works without a database connection. Execution adds connection
management, cancellation, timeouts, and output limits. MCP and REST expose the
same runtime services that Go applications can embed directly.

## Choose your entry point

| Tool or interface | Use it to |
| --- | --- |
| [`metis`](#run-from-source) | Validate models and run the standalone semantic runtime. |
| [MCP](#connect-an-mcp-client) | Give an agent tools for semantic discovery, SQL compilation, and bounded analytics over stdio or HTTP. |
| [REST](#serve-mcp-and-rest-over-http) | Integrate semantic discovery, compilation, explanation, and analytics into applications. |
| [`s2s`](#s2s-semantic-to-sql) | Compile semantic requests into SQL locally and validate, inspect, compare, or format model files. |
| [`s2sbench`](#s2sbench-agent-analytics-benchmarks) | Run repeatable agent analytics experiments and inspect correctness, readiness, and execution evidence. |
| [Go packages](#embed-in-a-go-application) | Compose a runtime with your own configuration, policies, and database integrations. |

## Run from source

Requires Go 1.25 or later. The default binary does not require CGO.

```sh
git clone https://github.com/meaningforge/metis.git
cd metis
go build -o bin/metis ./cmd/metis
bin/metis validate --project demo examples/demo/models/sales.ossie.yaml
```

The example runtime configuration is [examples/demo/metis.yaml](examples/demo/metis.yaml).
It registers a local project manifest that points to Ossie model files. Update
those files and your DataSource configuration locally, then restart the runtime
to apply changes.

## Connect an MCP client

A local MCP client can launch Metis as a stdio subprocess. Replace the absolute
paths below with the location of your checkout:

```json
{
  "mcpServers": {
    "metis": {
      "command": "/absolute/path/to/metis/bin/metis",
      "args": ["mcp", "--config", "/absolute/path/to/metis/examples/demo/metis.yaml"]
    }
  }
}
```

Start with a request such as: “In the demo project, compile total revenue by
region.” The agent can discover the model, select `total_revenue` and `region`,
and call `compile_sql`.

| MCP tools | Purpose |
| --- | --- |
| `list_projects`, `list_models`, `get_model` | Find and inspect available projects and models. |
| `list_metrics`, `get_metric` | Discover metrics and inspect their definitions. |
| `get_dimensions`, `get_dimension`, `get_relationships` | Find compatible dimensions, time grains, and relationships. |
| `search_ontology_concepts`, `resolve_ontology_concept` | Map business concepts to semantic assets. |
| `compile_sql` | Validate a semantic request and return SQL with an output schema. |
| `query_metrics`, `get_dimension_values` | Retrieve bounded metric results and live dimension values. |
| `compare_metrics` | Compare metrics across two periods, including values and changes. |
| `attribute_metric` | Decompose metric changes using supported additive or ratio metrics. |

Tools that retrieve data require a configured execution backend. Metric-change
attribution provides a numerical decomposition; interpreting its business causes
remains the caller's task.

## Serve MCP and REST over HTTP

Configure a shared bearer token and start the server:

```sh
export METIS_API_KEY='replace-with-your-own-secret'
bin/metis serve --config examples/demo/metis.yaml --addr 127.0.0.1:8080
```

Connect to `http://127.0.0.1:8080/mcp` with
`Authorization: Bearer <your-token>`. The REST endpoints under `/v1/**` use the
same authentication and semantic services. `/healthz` and `/readyz` are public
health endpoints. Local stdio runs under the subprocess owner's permissions.

For example, compile total revenue grouped by region:

```sh
curl -fsS http://127.0.0.1:8080/v1/compile-sql \
  -H "Authorization: Bearer $METIS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "dialect": "DUCKDB",
    "query": {
      "project": "demo",
      "model": "sales",
      "metrics": [{"name": "total_revenue"}],
      "dimensions": [{"name": "region"}]
    }
  }'
```

The response includes `sql_render_result` and `output_schema`. This endpoint
compiles the query without executing it. `/v1/explain` accepts the same request
shape and returns `SQLExplainResult`: semantic planning evidence, the same
`sql_render_result` and `output_schema` as Compile, and any compilation warnings.
Explain generates SQL without executing it.

## `s2s`: Semantic-to-SQL

`s2s` is the offline CLI for the Metis semantic compiler. Use it to test a model,
inspect semantic metadata, or generate SQL in a script without starting a server
or connecting to a database.

```sh
go build -o bin/s2s ./cmd/s2s
bin/s2s gen-sql \
  --model examples/demo/models/sales.ossie.yaml \
  --dialect DUCKDB --metric total_revenue --dimension region
```

Use `--dialect DORIS`, `CLICKHOUSE`, or `DUCKDB` to select a target. Add repeatable
`--metric`, `--dimension`, and `--filter` flags, or pass a structured request with
`--request-json`.

`gen-sql` outputs JSON containing `dialect`, `sql`, and optional `parameters`.
SQL retains its placeholders; pass parameter values in order to your database
driver. Values are never interpolated into SQL text.

| Command | Purpose |
| --- | --- |
| `gen-sql` | Compile a semantic request into SQL and parameters as JSON. |
| `validate-model`, `inspect` | Validate an Ossie document or inspect its metadata. |
| `validate-project`, `inspect-project` | Load and check a complete semantic project. |
| `diff-project` | Compare two local semantic project inputs. |
| `format-model` | Format a model file. |

```sh
bin/s2s validate-project --project demo --config examples/demo/project.yaml
bin/s2s inspect --model examples/demo/models/sales.ossie.yaml
```

## Execute queries

To enable execution, reference a named DataSource from your project registration
and define it in a local DataSource registry. The DataSource type selects the
database backend and SQL renderer.

The default build includes Doris and ClickHouse execution backends. To enable
DuckDB execution, build with CGO and the `duckdb` tag:

```sh
CGO_ENABLED=1 go build -tags duckdb -o bin/metis ./cmd/metis
```

DuckDB SQL compilation works with the default build. Compile-only deployments
require no database credentials. The execution runtime manages connections,
secrets, cancellation, timeouts, and output limits.

## `s2sbench`: Agent analytics benchmarks

S2SBench evaluates how an agent completes analytical tasks through semantic
interfaces. It runs frozen scenario suites, records attempts and query evidence,
and produces machine-readable reports. Use it to investigate whether a change to
Metis helps agents discover the right data and produce correct analytical results.

A run selects one interface: `metis-mcp` for Metis tools, or `okf` for
catalog-derived semantic files. Running the same suite with the same agent and
model through each interface enables a paired comparison.

Build with embedded DuckDB support and inspect the available commands:

```sh
make s2sbench-build
bin/s2sbench --help
bin/s2sbench run --help
```

Agent runs require an installed, authenticated agent CLI and model access. The
runner supports Codex, Claude Code, Pi, and a generic driver. The DuckDB build
also requires CGO and a C toolchain. Start with the `smoke` suite and set the
model identifiers to those used by your agent:

```sh
bin/s2sbench run \
  --suite smoke \
  --arm metis-mcp \
  --agent codex \
  --model '<model-id>' \
  --provider '<provider>' \
  --model-version '<model-version>' \
  --output ./s2sbench-results/smoke-metis
```

Completed runs contain `manifest.json`, `collection.json`, `attempts.jsonl`, and
`report.json`. Repeat an interrupted command with `--resume` to keep completed
work. Use `--detach` for a background run and `s2sbench stop <output-directory>`
to stop it.

For a paired experiment, repeat the run with `--arm okf` and a separate output
directory, then compare both collections:

```sh
bin/s2sbench analyze \
  --input ./s2sbench-results/smoke-okf \
  --input ./s2sbench-results/smoke-metis \
  --output ./s2sbench-results/comparison.json
```

Additional commands cover workload generation (`gen`), catalog-derived file
creation (`okfgen`), reports (`report`), and attribution and comparison
experiments. Use `bin/s2sbench <command> --help` for their inputs and options.
Agent benchmark runs are separate from the standard correctness tests.

## Use your own models

1. Copy [examples/demo](examples/demo) into your project directory.
2. Add your Ossie model files under `models/`.
3. Update `project.yaml` to select those files and register your project in
   `metis.yaml`.
4. Validate the project with `s2s validate-project`, then start `metis serve` or
   `metis mcp` with your runtime configuration.

Model paths are resolved relative to the project manifest. A runtime can register
multiple projects; see [examples/multi-project/metis.yaml](examples/multi-project/metis.yaml).

## Embed in a Go application

Import packages from `github.com/meaningforge/metis` and pin a version or
commit in your application's `go.mod`.

| Task | Entry point |
| --- | --- |
| Load a runtime from local configuration | `bootstrap.LoadRuntime` |
| Build a runtime from configuration and model bytes | `bootstrap.NewRuntime`, `RuntimeInput`, `ProjectInput` |
| Serve REST and MCP with an identity verifier | `hosting.NewHTTPHandler` |
| Configure project access and data constraints | `bootstrap.WithProjectAuthorizer`, `WithAssetVisibilityPolicy`, `WithDataAccessPolicy` |
| Supply database backends and secret resolution | `bootstrap.WithBackendRegistry`, `WithSecretResolver` |
| Load model documents from memory | `source.LoadProjectDocuments` |
| Replace an active semantic snapshot | `runtime.Manager.Replace` |
| Close database pools | `bootstrap.Runtime.Close` |

Runtime integration packages live under `app/bootstrap`, `app/hosting`,
`app/auth`, and `app/service`. Database extension interfaces live under
`renderer` and `execution`. Integrations use ordinary Go interfaces and
compile-time composition. Passing an explicit nil asset visibility policy
rejects runtime initialization. A request pinned again by the same runtime
keeps its snapshot; pinning it through another runtime creates a new scope
for that runtime.

The [embedding example](tests/integration/testdata/embedhost/main.go) constructs
a runtime from memory, applies access policies, and replaces a semantic snapshot.
Before v1, pin exact versions and run compatibility tests when upgrading.

## Development

The documentation and license checks require Python 3 in addition to Go.

```sh
go mod tidy
make check
make test-e2e
make test-duckdb-backend
```

`make check` runs documentation and source contract checks, `go vet`, unit tests,
sample models, semantic conformance tests, and the quickstart verification.
The Apache Ossie fixture check downloads a pinned upstream commit. For an offline
run, set `OSSIE_GIT_DIR` to a local Apache Ossie Git object directory containing
that commit. Database integration tests require explicitly configured services
and run separately.

## License

[Apache License 2.0](LICENSE). Third-party attribution and license texts are
available in [NOTICE](NOTICE), [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES), and
[licenses/](licenses/). Run `make licenses` after updating dependencies;
`make licenses-check` verifies the bundle included in release archives and images.

## Design and RFCs

See the [documentation guide](docs/README.md) for architecture, public contracts,
model authoring, execution, and S2SBench. [Core RFCs](docs/proposals/README.md)
record proposals and design rationale, with explicit lifecycle status.
