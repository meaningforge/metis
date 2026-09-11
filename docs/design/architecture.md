# Metis Core Architecture

Metis turns structured semantic intent into deterministic analytical queries.
Apache Ossie supplies model definitions. Agents own natural-language reasoning
and investigation strategy; Metis owns resolution, validation, planning, SQL
compilation, and optional bounded execution.

## Data flow

```text
Ossie source documents
  -> app/service/source -> manifest.SemanticManifest + SemanticGraph

query.SemanticQuery
  -> resolver -> planner/evaluation -> planner/semanticplan
  -> planner/conversion -> sqlplan.Plan
  -> selected renderer -> renderer/sql.SqlStatement + compiler/artifact.OutputSchema
  -> optional execution/runner -> Backend Driver -> analytical engine
```

The diagram summarizes ownership rather than every function call. The
[compilation specification](../specs/semantic/compilation-pipeline.md) describes
the semantic stages and their validation rules.

`query` holds requested metrics, dimensions, filters, ordering, and limits.
`sqlplan` is the structured SQL plan consumed by Renderers. `renderer/sql`
contains shared plan traversal, physical lowering helpers, and the completed
SQL and parameter types. Concrete Renderers supply dialect-specific behavior;
the shared package does not select Renderers or execute queries. A
`compiler/artifact.CompiledQuery` combines that query with its output schema.

SQL values remain parameterized. Both API responses and `s2s gen-sql` export
SQL together with parameters. Executors pass values to database drivers;
Metis does not scan SQL text to interpolate placeholders.

## Runtime and Project isolation

A Runtime can host multiple Projects. Each Project has a semantic namespace,
its own immutable generations, and at most one configured DataSource reference.
The Manager owns Project generation pointers and lazy request pinning. It is
not a connection pool or a DataSource. An explicit replacement rebuilds semantic
state while preserving process-owned execution resources.

Compile-only requests select a SQL dialect. Executed queries derive a Backend
from the resolved Project's DataSource type; that Backend supplies the exact
Renderer and DriverFactory. The same selected Renderer participates throughout
compilation. Model expressions do not choose database placement.

## Extension and interface boundaries

REST and MCP call shared `app/service/semantic` services. Project authorization,
asset visibility, and governed data constraints are enforced at those service
boundaries. Data constraints bind before semantic optimization. An omitted asset
visibility adapter uses the standalone default; explicitly passing nil, including
a typed nil, fails bootstrap.

`query_metrics` provides bounded semantic execution. `attribute_metric`,
`compare_metrics`, and `get_dimension_values` are typed workflows. They do not
provide an arbitrary-SQL execution endpoint. Analytics evaluation lives in
`analytics/attribution` and `analytics/comparison`.

Start at [bootstrap](../../app/bootstrap/), [semantic services](../../app/service/semantic/),
[compiler](../../compiler/), [Renderer](../../renderer/), or
[execution](../../execution/) for the corresponding implementation.
