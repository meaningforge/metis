# Metis Glossary

This glossary fixes the canonical vocabulary used across specifications, public contracts, tests, and contributor documentation. It is descriptive of current Metis concepts; compatibility promises are defined by [`public-contract.md`](public-contract.md).

## Semantic model

An Apache Ossie semantic model loaded into a Metis project. Ossie remains the source of truth for standard semantic fields. Metis-specific behavior is expressed only through explicit extensions or runtime configuration.

## SemanticManifest

The immutable, versioned, project-scoped semantic read model built from
validated Ossie documents and derived semantic indexes. Bootstrap may merge
several project manifests into one `SemanticManifest` without merging their
project namespaces. Resolver, discovery, and planning read the same current
manifest. `SemanticManifest` is distinct from an external physical metadata
Catalog such as AWS Glue or Apache Gravitino.

## Semantic intent

A structured analytical request expressed in semantic terms such as metrics, dimensions, filters, grain, project, and model. Semantic intent is the input to Metis resolution and planning; it is not SQL.

## Semantic plan

The canonical, deterministic, engine-neutral result of Metis planning, and the semantic computation DAG itself: `SemanticPlan.Nodes` are ordered topologically and each node's `NodeBase().Inputs` are its dependency edges. It is the single IR consumed by validation, optimization, explain, fingerprinting, and SQL lowering. RFC-0037 removed the separate graph wrapper that used to hold the DAG for some queries; there is no second name for it.

## Semantic plan node

A `SemanticPlanNode` in a `SemanticPlan` DAG represents one semantic computation or transformation step. Its concrete node type is the semantic discriminator, and `SemanticPlanNodeBase.Inputs` records dependency ordering and producer grain.

## Metric evaluation plan

The static, query-scoped, metric-only compiler IR built after semantic
resolution and before source-aware semantic planning. A
`MetricEvaluationPlan` owns why metrics are required, one
`MetricEvaluationNode` identity per metric, metric-to-metric
`MetricEvaluationInput` edges, stable metric evaluation kinds, typed specs,
resolved expressions, and canonical time bindings. It owns no dataset roots,
relationship paths, predicate placement, semantic-node shape, source-sharing
groups, or SQL structure.

Metric-bearing queries build exactly one validated metric evaluation plan.
Metric-free queries do not fabricate one.

## Metric evaluation root

A query obligation identifying a metric and why it is required. The canonical
roles are `output`, `predicate`, and `order`. A metric may have more than one
role without receiving more than one metric evaluation node.

## Metric evaluation node

Exactly one metric's query-scoped semantic identity inside a
`MetricEvaluationPlan`. It is not a source scan, semantic plan node,
aggregation, SQL query block, or CTE. Multiple metric evaluation nodes may
share later semantic or physical work without merging their identities.

## Metric attribution plan

The query-scoped proof that one governed metric admits an exact
additive or ratio change decomposition for explicit baseline/current periods.
It derives from `MetricEvaluationPlan`; it does not replace metric dependency
authority. Only its evaluated result, not the plan, is Agent-facing.

## Metric attribution bundle

A bounded collection of independent one-dimension attribution plans and
physical queries that share metric, periods, filters, model, and selected
Renderer dialect.
It is not a combined multi-dimensional grain and defines no cross-dimension
ranking.

## Attribution evaluator

The focused `analytics/attribution` runtime component that validates complete
normalized bundle evidence, reconciles additive or ratio algebra with
arbitrary-precision arithmetic, and returns the closed `AttributeMetricResult`.
It owns no SQL, SemanticManifest, Renderer, Driver, or database connection.

## Metric comparison

The closed analytical workflow that evaluates the same governed metrics and
shared dimension grain over two exact periods. It aligns the union of complete
dimension tuples and returns explicit baseline/current presence, values, delta,
and percent change. It does not attribute contribution or infer causality.

## Comparison evaluator

The focused `analytics/comparison` runtime component that validates two
complete normalized evidence sets, preserves absence/null/zero distinctions,
aligns composite tuples, and computes exact change with arbitrary-precision
arithmetic. It owns no SQL, SemanticManifest, Renderer, Driver, or database
connection.

## Semantic evaluation

The generic semantic meaning computed by a plan node or DAG. It is not the name
of an additional `SemanticEvaluation` IR and must not be used as an alias for
the specifically defined metric-only `MetricEvaluationPlan`.

## Relationship

An Ossie semantic-model connection between datasets. Relationships define dataset connectivity and join semantics. They are distinct from conversion-metric `entity` links.

## Conversion entity

The Ossie conversion-metric identity link that associates a base event with a conversion event. The canonical serialized field is `entity`; in Go it is `ConversionMetricSpec.Entity` with a `ConversionPropertyPair` value.

## Grain

The semantic grouping level at which a value is defined, produced, or compared. A time grain can be `day`; an output grain can include multiple grouping dimensions such as `country + day`. `grain` is the canonical Metis term and serialized field name.

## Proof

A machine-checkable justification that authorizes a correctness-sensitive transformation, placement, or planning decision. If required proof cannot be established, the operation must not be applied.

## Evidence

A structured semantic fact retained for explanation, validation, lineage, auditing, or regression checks. Evidence can record a proof result but does not itself authorize a transformation unless the relevant contract explicitly says so.

## Population preservation

A planner proof that a relationship traversal leaves a metric's aggregated row
population intact. It is independent of duplicate invariance and requires
complete evidence for join-row retention, filter placement, grouping, and
expression references. Target-key uniqueness proves no fan-out; it does not by
itself prove population preservation.

## SQLDialect

The compile-only identity of an output SQL language, such as Doris or DuckDB.
It resolves one Renderer and does not select a database instance.

## Renderer

The dialect-specific physical SQL implementation selected exactly once per
compilation. The same Renderer identity/instance supplies expression dialect
evidence, capabilities, and final SQLPlan rendering.

## Compile target (removed)

The former `CompileTarget{Engine, Dialect}` authority, removed by the Renderer
authority migration. SQLDialect selects one Renderer directly; no replacement
engine/target alias exists.

## SQLPlan

The canonical typed, validated physical SQL planning IR between `SemanticPlan`
and the selected Renderer. It owns the root query block, explicit block
dependencies, and typed physical expressions and relations. SQLPlan is not
Agent-facing and does not contain execution connections, credentials, or an
independent Renderer/target authority.

## Execution binding (transitional)

The former project/model placement alias. [Core runtime contract](operations/runtime-bootstrap.md) removes it in favor of a
root Project Registration applying named DataSources and model-level selection.
New semantic
or runtime contracts must not depend on `ExecutionBinding`.

## Backend

One type-level executable database-family implementation registered by
DataSource type. It binds one exact Renderer and DriverFactory; SQLDialect
derives only from that Renderer. It owns no concrete endpoint, credentials, or
per-instance policy.

## DataSource

One named deployment-scoped database/warehouse instance configuration. It owns
a `type`, instance configuration, SecretRefs, and bounded execution policy. A
DataSource is not a semantic asset, Backend, connection handle, or Catalog.

## Project registration

The root deployment entry addressed by Project Key/Name. It references one
semantic project manifest and may apply zero or more DataSources. Project
resolution is explicit project, then configured `default_project`, then the
sole registered project, otherwise `PROJECT_REQUIRED`.

## Physical query

The dialect-specific executable query produced by compilation. SQL is one
physical-query representation. Optional Execution Runtime may execute it only
together with its OutputSchema and without consulting semantic/compiler state.

## Output schema

The target-neutral description of result-column order and semantic metadata produced alongside a physical query.

## Semantic context

A bounded, structured subset of semantic facts returned for Agent reasoning. Semantic context is derived from the same SemanticManifest and resolver truth used by planning and compilation.

## Explain

A compile-only result (`SQLExplainResult`) combining stable semantic evidence
(`QueryExplanation`) with rendered SQL (`sql_render_result`), output schema, and
optional compilation warnings. Metis Explain is not database SQL `EXPLAIN` and
does not expose raw planner graphs, SQLPlan, or renderer-private state.

## s2s

The canonical name of the local/offline Semantic-to-SQL compiler CLI. The public command vocabulary is `s2s gen-sql`, `s2s validate-model`, and `s2s inspect`.
