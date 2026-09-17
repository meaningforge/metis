# Core RFCs

Community RFCs and retained Core design history. Drafts invite discussion; accepted or implemented designs must also be reflected in current specifications. Historical APIs and alternatives may differ from the current implementation.

## Contributing an RFC

Open a pull request with a document based on [the template](TEMPLATE.md). Explain the problem, proposed contract, alternatives, compatibility, and verification. Discuss substantive changes before implementing them. Acceptance is not evidence of implementation.

States are **Draft**, **Accepted**, **Implemented**, **Rejected**, and **Superseded**. Preserve original identifiers when importing a Core design; numbering gaps are intentional. New RFC numbers are assigned during review to avoid collisions.

Only engine, semantic-model, query-interface, and developer-tool designs are included. See the [documentation guide](../README.md) for historical vocabulary and current authority.

## Execution

- [RFC-0045: Multi-Project Runtime Bootstrap](execution/0045-multi-project-runtime-bootstrap.md) — Implemented
- [RFC-0054: Execution Runtime and Analytics Runtime Architecture](execution/0054-execution-analytics-runtime-architecture.md) — Implemented
- [RFC-0056: Runtime Metric Query Execution](execution/0056-runtime-metric-query-execution.md) — Implemented
- [RFC-0057: Production Metric Attribution Runtime](execution/0057-production-metric-attribution-runtime.md) — Implemented
- [RFC-0059: Production Execution Runtime Resource Lifecycle and Concurrency Control](execution/0059-production-execution-runtime-resource-lifecycle-and-concurrency-control.md) — Implemented
- [RFC-0060: Warehouse Renderer and Execution Backend Extension Architecture](execution/0060-warehouse-renderer-and-execution-backend-extension-architecture.md) — Implemented
- [RFC-0063: DuckDB Execution Backend](execution/0063-duckdb-execution-backend.md) — Implemented
- [RFC-0064: Production Metric Comparison Runtime](execution/0064-production-metric-comparison-runtime.md) — Implemented
- [RFC-0065: Flat String DataSource Config and External References](execution/0065-flat-string-datasource-config.md) — Implemented
- [RFC-0067: Governed Dimension Value Discovery](execution/0067-governed-dimension-value-discovery.md) — Implemented
- [RFC-0069: Runtime Capability and Result Diagnostics](execution/0069-runtime-capability-and-result-diagnostics.md) — Implemented
- [RFC-0081: Execution Backend Resilience Conformance](execution/0081-execution-backend-resilience-conformance.md) — Implemented
- [RFC-0086: Federated Semantic Query Execution](execution/0086-federated-semantic-query-execution.md) — Draft

## Interfaces

- [RFC-0012: Compiled Output Schema](interfaces/0012-compiled-output-schema.md) — Implemented
- [RFC-0016: Agent Semantic Context and Explainability](interfaces/0016-agent-semantic-context-explainability.md) — Implemented
- [RFC-0018: Agent Query Contract and Structured Diagnostics](interfaces/0018-agent-query-contract-diagnostics.md) — Implemented
- [RFC-0019: Agent Semantic Discovery and Context Quality](interfaces/0019-agent-semantic-discovery-context-quality.md) — Implemented
- [RFC-0032: Transport-neutral Error Classes](interfaces/0032-transport-neutral-error-classes.md) — Superseded
- [RFC-0034: Caller-action Error Contract](interfaces/0034-caller-action-error-contract.md) — Implemented
- [RFC-0044: Staged Agent Semantic Query Surface](interfaces/0044-primary-agent-semantic-surface.md) — Implemented
- [RFC-0050: Metric-free Agent discovery and sufficient semantic evidence](interfaces/0050-metric-free-agent-discovery.md) — Implemented
- [RFC-0051: Progressive Agent Semantic Tools](interfaces/0051-progressive-agent-semantic-tools.md) — Implemented
- [RFC-0066: dbt-aligned semantic discovery](interfaces/0066-dbt-aligned-semantic-discovery.md) — Implemented
- [RFC-0071: Governed Agent Metric Aliases and Candidate Differences](interfaces/0071-governed-agent-metric-aliases.md) — Implemented
- [RFC-0080: Large Semantic Estate Discovery](interfaces/0080-large-semantic-estate-discovery.md) — Implemented
- [RFC-0083: Local Stdio MCP Transport](interfaces/0083-local-stdio-mcp-transport.md) — Implemented

## Semantic

- [RFC-0013: Time Offset Alignment Semantics](semantic/0013-time-offset-alignment.md) — Implemented
- [RFC-0014: Metric Fill Policy](semantic/0014-metric-fill-policy.md) — Implemented
- [RFC-0015: Semi-Additive Rollup and Composability](semantic/0015-semi-additive-rollup-composability.md) — Implemented
- [RFC-0017: Semantic Optimizer Safety and Query Shape Quality](semantic/0017-semantic-optimizer-safety-query-shape.md) — Implemented
- [RFC-0020: Semantic Plan Quality Foundations](semantic/0020-semantic-plan-quality.md) — Implemented
- [RFC-0021: Semantic-Critical Extension Capability Contract](semantic/0021-semantic-critical-extension-capabilities.md) — Implemented
- [RFC-0022: Typed Expression Function Resolution](semantic/0022-typed-expression-function-resolution.md) — Implemented
- [RFC-0023: Explicit Cast and Nullability Semantics](semantic/0023-explicit-cast-nullability.md) — Implemented
- [RFC-0024: Scope-aware Symbol Binding](semantic/0024-scope-aware-symbol-binding.md) — Implemented
- [RFC-0025: Typed Semi-structured Path Semantics](semantic/0025-typed-semi-structured-path-semantics.md) — Implemented
- [RFC-0026: Multi-metric Shared-grain Planning](semantic/0026-multi-metric-shared-grain-planning.md) — Implemented
- [RFC-0027: Deterministic Semantic Optimizer](semantic/0027-deterministic-semantic-optimizer.md) — Implemented
- [RFC-0028: Semantic-Critical Metric Scale Lowering](semantic/0028-semantic-critical-metric-scale.md) — Implemented
- [RFC-0029: Semantic Logical Stage Model](semantic/0029-semantic-logical-stage-model.md) — Implemented
- [RFC-0030: SemanticPlan Architecture](semantic/0030-semantic-plan-architecture.md) — Implemented
- [RFC-0031: Typed Semantic Evaluation Payloads](semantic/0031-typed-semantic-evaluation-payloads.md) — Superseded
- [RFC-0033: Aggregation Algebra and Fan-out Safety](semantic/0033-aggregation-algebra-and-fanout-safety.md) — Implemented
- [RFC-0037: Universal SemanticPlan DAG and Unified Lowering](semantic/0037-unified-semantic-lowering.md) — Implemented
- [RFC-0039: Query-Scoped MetricEvaluationPlan and Explicit Semantic Lowering](semantic/0039-metric-evaluation-plan.md) — Implemented
- [RFC-0040: Typed SemanticPlan Nodes](semantic/0040-typed-semantic-plan-nodes.md) — Implemented
- [RFC-0052: Deterministic Metric Change Attribution](semantic/0052-deterministic-metric-change-attribution.md) — Implemented
- [RFC-0058: Semantic Core Layering, Lookup, and Manifest-Scoped SemanticGraph](semantic/0058-semantic-core-layering-and-semantic-graph.md) — Implemented
- [RFC-0061: Planner Package Responsibilities and IR Conversion Layout](semantic/0061-planner-package-responsibilities.md) — Implemented
- [RFC-0076: Deterministic Model Quality Diagnostics](semantic/0076-deterministic-model-quality-diagnostics.md) — Implemented
- [RFC-0085: Ontology-assisted Semantic Resolution Evidence](semantic/0085-ontology-assisted-semantic-resolution.md) — Implemented

## SQL

- [RFC-0035: Concrete SQL Dialect Rendering](sql/0035-concrete-sql-dialect-rendering.md) — Implemented
- [RFC-0038: Typed SQLPlan and Physical SQL Lowering](sql/0038-typed-sql-plan.md) — Implemented
- [RFC-0062: Compiler and Physical Query Package Responsibilities](sql/0062-compiler-and-physical-query-package-responsibilities.md) — Implemented

## Tooling

- [RFC-0011: Executable Semantic Correctness Corpus](tooling/0011-executable-semantic-correctness-corpus.md) — Implemented
- [RFC-0036: Runtime Observability Contract](tooling/0036-runtime-observability-contract.md) — Implemented
- [RFC-0041: AgentBench live-run reliability and incremental evidence](tooling/0041-agentbench-live-run-reliability.md) — Implemented
- [RFC-0042: AgentBench multi-model semantic world](tooling/0042-agentbench-multi-model-semantic-world.md) — Implemented
- [RFC-0043: AgentBench compact result artifacts](tooling/0043-agentbench-compact-result-artifacts.md) — Implemented
- [RFC-0046: AgentBench multi-turn Agent sessions](tooling/0046-agentbench-multi-turn-agent-sessions.md) — Superseded
- [RFC-0048: AgentBench question-isolated Agent sessions](tooling/0048-agentbench-question-isolated-sessions.md) — Implemented
- [RFC-0049: AgentBench discovery sufficiency and budget audit correctness](tooling/0049-agentbench-discovery-budget-audit.md) — Implemented
- [RFC-0053: AgentBench Attribution Evaluation](tooling/0053-agentbench-attribution-evaluation.md) — Implemented
- [RFC-0068: OKF-Ossie Agent SQL Readiness Evaluation](tooling/0068-okf-ossie-agent-sql-readiness-evaluation.md) — Superseded
- [RFC-0070: Production Runner Real-Engine Conformance](tooling/0070-production-runner-real-engine-conformance.md) — Implemented
- [RFC-0072: Unified AgentBench Semantic-Interface Frame](tooling/0072-unified-agentbench-semantic-interface-frame.md) — Implemented
- [RFC-0073: S2SBench Oracle Authority and Validation Tiers](tooling/0073-s2sbench-oracle-authority-and-validation.md) — Draft
- [RFC-0074: S2SBench diagnostic fidelity and token efficiency](tooling/0074-s2sbench-diagnostic-fidelity-and-token-efficiency.md) — Implemented
- [RFC-0082: Promote S2SBench to a Community CLI](tooling/0082-s2sbench-community-cli.md) — Implemented
