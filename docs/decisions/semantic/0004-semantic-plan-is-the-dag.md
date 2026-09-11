# ADR-0004: SemanticPlan Is the Semantic DAG

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-23
**Last reviewed:** 2026-08-25

## Context

Metis had two canonical lowering authorities.

`SemanticPlan` was canonical for every query, but staged computation lived
behind an optional `Graph *SemanticGraph`. `planner.BuildSQLAST` branched on
whether that wrapper existed:

1. staged queries lowered from `SemanticGraph`;
2. everything else lowered directly from plan-level projections, joins,
   predicates, groups, sorts, and limit.

At the time the split was measured, 37 of the 96 core conformance scenarios
reached lowering through the second path — not exotic shapes, but simple
metrics, aggregation variants, same-source multiple metrics, one-hop joins, and
temporal joins.

Both interpreters produced correct SQL. That was the problem. Two independent
implementations could interpret resolved projections, relationship and temporal
join semantics, predicate ownership, grouping and time grain, output ordering
and limits, and extension-derived behavior differently. A new semantic feature
could work on staged queries while being absent or subtly different on direct
ones. Counting scenarios detected a routing change; it could not detect drift
inside either branch.

The wrapper had also outlived its purpose. `SemanticGraph` originally separated
staged composition from the flat plan, and that boundary made sense while graph
ownership was optional. Once every plan owned exactly one DAG, plan and graph
shared a construction point, owner, lifetime, validation boundary, clone
boundary, optimizer boundary, explain boundary, fingerprint boundary, and
lowering boundary — two names and two API surfaces for one architectural
responsibility.

## Decision

**`SemanticPlan` is the semantic computation DAG.** `SemanticPlan.Nodes` are
the nodes, each node's `Inputs` are its dependency edges, node order is
topological, and a `SemanticOutputContract` owns the final projections, grain,
predicates, ordering, and limit.

Every successful `Planner.Plan` result owns that DAG. Metric-bearing queries
own the typed evaluation nodes metric construction already produced;
metric-free dimension and `distinct_values` queries own a typed
source-selection node whose mode is the distinction between them, rather than
a fabricated metric payload or an unvalidated boolean outside the node.

`SemanticGraph`, `SemanticPlan.Graph`, the graph clone and accessor APIs,
`ValidateSemanticGraph`, `FingerprintSemanticGraph`, and the direct interpreter
are deleted. `resolver.RequiresMetricStaging` is no longer a routing contract:
the planner derives what construction built rather than reading a flag the
resolver set and the planner then wrote back to.

Physical lowering has one semantic entry and no alternate interpreter.
`BuildSQLPlan` selects a physical shape — compact for a plan expressible as one
query block, composed for one needing separate blocks — from the validated
nodes and their boundaries. That choice affects SQL shape only; ADR-0005 owns
the subsequent physical renderer boundary.

## Consequences

**Every consumer observes the same meaning.** Validation, optimization,
explain, lineage, fingerprinting, and lowering read one plan-owned DAG. Rules
that previously ran only for staged queries now run for all of them: the DAG
contract — canonical set ordering, boundary/kind agreement, duplicate inputs,
per-node dataset reachability — used to be checked on 59 of 96 scenarios and
is now checked on all 96.

**Explain covers every query.** The 37 compact scenarios were previously
inexplicable, which meant the queries whose source boundary and predicate
placement are least visible in their SQL were the ones the plan would not
describe.

**Compact SQL is preserved.** A universal plan DAG does not require universal
composed SQL shape. The 37/59 compact/composed split is recorded as physical-shape
diagnostics rather than asserted as a semantic boundary.

**Two fingerprint contracts stay deliberately different.** The 288 compiled-SQL
fingerprints are byte-stable and remained byte-identical through the entire
migration. `FingerprintSemanticPlan` describes IR structure and moved each time
ownership moved, which is the intended signal rather than a regression; each
movement required an explicit projection disposition and review.

**The optimizer's surface did not widen.** Flattening the DAG into
`SemanticPlan` did not grant rules arbitrary plan access. A node rewrite still
receives a bounded, transient, unexported rewrite state, not the plan.

**Query-shape fields remain construction inputs.** `Root`, `Joins`,
`Projections`, `Predicates`, `Groups`, and `Sorts` still carry resolved query
structure into source and node planning. They are no longer downstream
semantic authority, and no lowering path reads them.

## Alternatives considered

**Keep the permanent direct/staged split.** Minimal migration cost, but leaves
two semantic interpreters and makes every future feature prove parity twice.

**Keep a mandatory one-to-one `SemanticGraph` wrapper.** Preserves
graph-focused APIs, but every plan would own exactly one graph with the same
lifetime and authority, and paired plan/graph entry points would persist for a
single canonical object.

**Rename the graph to `SemanticDataflow` or add a `DataflowPlan`.** Resembles
MetricFlow vocabulary without adding structural capability, and suggests
generic data movement rather than semantic evaluation boundaries.

**Model every relational operation as a semantic plan node.** A generic
scan/filter/project/join/aggregate algebra would duplicate the physical
SQLPlan, invite physical planning responsibilities, and weaken the
semantic-node boundary.

## References

- RFC-0037, *Universal SemanticPlan DAG and Unified Lowering* (`docs/proposals/semantic/0037-unified-semantic-lowering.md`, tracked in PR #406)
- [`../../specs/semantic/semantic-plan-node-model.md`](../../specs/semantic/semantic-plan-node-model.md)
- [`../../specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md)
- [`../../design/semantic/pipeline.md`](../../design/semantic/pipeline.md)
