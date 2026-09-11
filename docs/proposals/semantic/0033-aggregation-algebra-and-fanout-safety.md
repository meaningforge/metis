# RFC-0033: Aggregation Algebra and Fan-out Safety

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-21
- **Last updated:** 2026-08-25
- **Scope:** `expression` typed analysis, `catalog` metric analysis, `planner` fan-out and rollup decisions
- **Supersedes:** None

## Summary

Model the algebraic properties of aggregations so the planner can decide fan-out
safety and rollup legality semantically instead of structurally. Two properties
are derived from the expression — duplicate sensitivity and rollup algebra — and
consumed by a third, plan-owned decision about derived evaluation grain. Every
property is derived, never declared. Anything not derivable, and any rollup whose
required intermediate state is not retained, fails closed.

## Motivation

Metis already models aggregation *kind*. `expression.FunctionKind` separates
`SCALAR` from `AGGREGATE`, `AggregationState` carries `SCALAR` / `AGGREGATE` /
`MIXED` on every `TypedExpression`, the signature registry marks `SUM`, `SUMIF`,
`AVG`, `MIN`, `MAX`, and `COUNT` as aggregates, and the analyzer rejects a nested
aggregate with `INVALID_AGGREGATION`.

What is absent is the *algebra*. Knowing a function aggregates says nothing about
how it behaves when a join duplicates rows, or when a result must be produced at
a coarser grain than its inputs. No distributivity, decomposability,
duplicate-sensitivity, partial-state, or merge-operator concept exists anywhere
in the tree.

The planner therefore approximates. `planner/relationship_fanout.go` rejects a
traversal whenever the target join columns are not proven unique by
`primary_key` or `unique_keys`, without consulting the metric at all. Two
consequences follow:

- **Over-restriction.** Aggregations that are invariant under row duplication are
  rejected on join shape alone, even when the result would be correct.
- **Under-protection.** Rollup to a coarser grain has no general guard. RFC-0015
  established a fail-closed rollup contract, but only for metrics explicitly
  declared semi-additive. An ordinary holistic aggregate has no equivalent
  protection.

Both failure modes are in the class Metis exists to prevent, and both are
currently decided by proxy rather than by semantics.

## Design

### Layering

The three concerns belong to different layers and must not be presented as one
kind of thing:

```text
expression / catalog   derived properties     1. duplicate sensitivity
                                              2. rollup algebra
                              |
                              v
planner                plan-owned decision    3. derived evaluation grain
```

Properties 1 and 2 are functions of the expression and are derivable from it.
Decision 3 is not an expression property at all: whether a derived metric
recomputes its dependencies at the target grain or aggregates already-computed
finer-grain results is an evaluation strategy chosen from the stage graph and the
dependency graph. It **consumes** properties 1 and 2; it is not one of them.

### Property 1: duplicate sensitivity

The definition must be stated precisely, because the useful case and the easy
case differ.

> An aggregation is **duplicate-invariant** when its result is unchanged under
> arbitrary per-row multiplicity: replacing each input row `r_i` with `m_i`
> identical copies, for any `m_i >= 1` chosen independently per row.

Uniform replication — every row copied the same number of times — is a strictly
weaker condition, and it is *not* what a fan-out join produces. Fan-out
multiplicity varies per row with the number of matching rows on the joined side.
Classification is therefore defined over arbitrary per-row multiplicity.

| Class | Aggregations | Why |
| --- | --- | --- |
| `Sensitive` | `SUM`, `SUMIF`, `COUNT`, `AVG` | `AVG` is invariant only under uniform replication; varying `m_i` reweights the mean |
| `Invariant` | `MIN`, `MAX`, `COUNT` over a `DISTINCT` argument | the value set is unchanged; only multiplicities change |
| `Unknown` | not derivable | fails closed |

Row *elimination* (`m_i = 0`, as an inner join can cause) is deliberately outside
this definition. That is a change of population, not duplication, and is handled
by the separate population-preservation obligation below.

### Property 2: rollup algebra

A classification alone does not prove a rollup is safe. The property is a triple —
partial-state shape, merge operator, and finalizer — because the merge operator is
frequently not the original aggregate.

| Aggregation | Class | Partial state | Merge | Finalize |
| --- | --- | --- | --- | --- |
| `SUM` | `Distributive` | `sum` | `SUM` | identity |
| `SUMIF` | `Distributive` | conditional `sum` | `SUM` | identity |
| `MIN` / `MAX` | `Distributive` | `value` | `MIN` / `MAX` | identity |
| `COUNT` | `Distributive` | `count` | **`SUM`** | identity |
| `AVG` | `Algebraic` | `(sum, count)` | componentwise `SUM` | `sum / count` |
| `COUNT` over `DISTINCT` | `Holistic` | no bounded exact state | — | — |
| unresolved | `Unknown` | — | — | — |

`COUNT` merging through `SUM` rather than through `COUNT` is the canonical trap
this table exists to prevent.

**Classification is necessary but not sufficient.** A rollup is permitted only
when *both* hold:

1. the algebra admits a merge, and
2. the plan demonstrably retains the required partial state at the finer grain.

An `AVG` computed and materialized as a scalar mean has discarded `(sum, count)`
and is no longer rollupable, even though `AVG` is classified `Algebraic`. This is
the same rule RFC-0015 already applies to ordered-value state, generalized: state
that cannot reproduce the semantics at the coarser grain is non-composable, and
the planner must fail closed rather than apply a plain aggregate to a finished
value.

`Holistic` and `Unknown` never admit a rollup.

### Decision 3: derived evaluation grain

Owned by the planner, resolved from the stage graph and metric dependency graph.

Arithmetic over dependencies at a shared grain stays legal and unchanged; this
RFC does not restrict it. The guarded case is specifically cross-grain rollup of
already-computed values — including re-aggregating a computed ratio instead of
recomputing it from its parts — which is permitted only when property 2 and its
state-retention condition are both satisfied.

### Derivation: sources and trust

RFC-0015 established that composability is derived from the plan rather than
declared in YAML. This RFC keeps that rule. Ossie `Metric` carries only
`ai_context`, `custom_extensions`, `datatype`, `description`, `expression`, and
`name`; no canonical aggregation-property field exists and none should be added.

Derivation sources, in order:

1. the function signature registry, for aggregates Metis knows;
2. **argument shape**, because distinctness is not part of the function name.
   `expression/parser.go` parses `DISTINCT` as a unary operator, so
   `COUNT(DISTINCT x)` is `COUNT` applied to `UnaryExpr{Op: "DISTINCT"}`. A
   property table keyed on function name alone cannot express this and must not
   be built that way;
3. a **registered extension interpreter**, under the RFC-0021 capability
   contract, for target-native aggregations Metis does not itself model;
4. otherwise `Unknown`.

Source 3 is deliberately narrow. **An Ossie vendor extension cannot assert its
own safety.** Opaque extensions are preserved, as the extension-preserving
invariant requires, but a preserved payload is untrusted input: if a model author
could write `duplicate_invariant: true` into `custom_extensions` and thereby
unlock a fan-out traversal, the fail-closed guarantee would be author-defeatable.
Safety evidence may originate only from an interpreter registered in the
extension registry and resolved for the selected compile target, and the evidence
is attributed to that interpreter rather than to the model file. An unregistered
or opaque extension contributes no property and resolves `Unknown`.

### Unknown functions must not be read as an assertion

`catalog/expression_analyzer_native.go` enables `AllowUnknownFunctions`, and an
unknown function is currently typed `Aggregation: AggregationScalar` with
`Type: TypeUnknown`. That default is a placeholder, not a claim: a target-native
aggregate such as `uniqExact(...)` or `quantile(...)` is today classified as
scalar.

Property resolution must therefore key on derivation success, not on the
analyzer's default. An unresolved function yields `Unknown` on both properties,
and `Unknown` fails closed wherever a property is required.

### Planner decisions

**Fan-out.** Duplicate invariance is **necessary but not sufficient** to relax
the current guard. Relaxation additionally requires proof that the join does not
change the aggregated population — that filter placement, grouping, expression
references, and join-side row elimination leave the aggregated set intact. Absent
that proof the existing structural rejection stands. A blanket "distinct
aggregates are safe under fan-out" rule would be incorrect.

**Rollup.** Permitted only under property 2 *and* retained partial state.
`Holistic`, `Unknown`, and discarded state fail closed with a typed error rather
than silently re-aggregating.

Every decision is a correctness decision. No property here estimates or compares
execution cost.

## Alternatives

- **A single additivity class.** Rejected: it conflates duplicate sensitivity
  with rollup algebra, which disagree on `COUNT(DISTINCT ...)`.
- **Rollup as a classification enum only.** Rejected: without a merge operator
  and partial-state shape the label does not prove anything, and it hides the
  `COUNT`-merges-via-`SUM` case.
- **Treating derived evaluation grain as an expression property.** Rejected: it
  is an evaluation strategy determined by the plan, not a function of the
  expression.
- **A declared property in Ossie, or a self-describing vendor extension.**
  Rejected: it contradicts RFC-0015's derived model and makes the fail-closed
  guarantee defeatable by the model author.
- **Defining duplicate invariance over uniform replication.** Rejected: fan-out
  produces per-row multiplicity, so the weaker definition would misclassify `AVG`
  as safe.
- **Defaulting unknown aggregates to distributive.** Rejected: it converts a
  missing proof into a silent claim.
- **Leaving the structural fan-out guard as-is.** Rejected: it is simultaneously
  too strict and too permissive, and neither error is visible to the caller.

## Rollout and migration

The property model is additive to typed analysis and changes no existing
compiled output on its own.

Sequenced so that tightening and relaxation are separately reviewable:

1. derive and expose the properties, asserted by tests, with no planner behavior
   change;
2. add fail-closed rollup and derived-composition guards, which may reject
   queries that compile today — each such query is a correctness fix and must be
   named in the change;
3. relax the fan-out guard only where population preservation is proven, which
   only accepts queries rejected today.

Steps 2 and 3 must not land together. Any compiled-output or fingerprint change
must be declared as a correctness fix rather than absorbed silently.

## Rollout progress

**Step 1 — derive and expose the properties.** Landed. `expression` derives
duplicate sensitivity and rollup algebra per call site; `catalog` records them on
every `AnalyzedExpression`. No planner behavior changed.

**Step 2 — fail-closed rollup guards.** Landed, and it found what this RFC
predicted it would. `planner/metric_evaluation_sql.go` lowered every cumulative
metric as `SUM(<base metric column>) OVER (...)` with the operator hardcoded,
consulting nothing about what the base aggregated. Reproduced before changing
anything, on the commerce fixture with a `MAX(orders.amount)` base:

```sql
SUM(`metric_001_peak_order`.`peak_order`) OVER (
  ORDER BY `metric_001_peak_order`.`order_date`
  ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
)
```

A running maximum compiled to a running *sum of per-period maxima*. Nothing
rejected it, and the number looks like an answer.

The guard resolves the merge operator from the base metric's derived properties
instead, using the analyzed expression of the dialect the resolver selected —
because a metric may declare an ANSI `COUNT(DISTINCT x)` alongside a
target-native `uniqExact`, and the one that governs is the one being compiled.

Mergeability requires three things, each ruling out a distinct way of being
silently wrong:

- exactly one aggregation, so there is a single partial state to merge;
- `Distributive` algebra;
- the emitted column *is* the partial state — one component, identity finalize.

The third is the RFC's "retained partial state" requirement, and it is what
rejects `AVG`. `AVG` is algebraic and could merge from `(sum, count)`, but the
evaluated column holds the finished ratio and those partials are already gone.

**Compiled-output change, declared rather than absorbed.** A cumulative metric
over a `MIN` or `MAX` base now emits `MIN`/`MAX` as its window function instead
of `SUM`. That is a correctness fix: the previous output computed a different
quantity than the metric named. Output for `SUM` and `COUNT` bases is unchanged,
because `SUM` was already their correct merge operator — `COUNT` merging through
`SUM` is the case the property table exists to get right.

**Queries that compiled and now fail closed.** A cumulative metric over an `AVG`
base, over a `COUNT(DISTINCT ...)` base, over a function Metis does not model,
or over any non-source-aggregate base. Each returns `INVALID_METRIC_ROLLUP`
naming the base metric, its aggregation, and its rollup algebra. All are
correctness fixes: none of those had a correct compiled form to preserve.

**A latent defect surfaced by the change.** `cloneSemanticStageEvaluation`
replaced `Evaluation.Source` with a fresh `&SourceMetricEvaluation{}` rather than
copying it. That was invisible while the struct was empty and silently discarded
the rollup proof the moment it carried one, which made every legitimate
cumulative metric fail closed until the clone was fixed. Recorded because the
same shape exists wherever a clone enumerates fields instead of copying a value.

**Step 3 — fan-out relaxation.** Landed with a deliberately narrow first
admission rule. A single fan-out relationship is accepted only when a
null-rejecting joined-side filter defines existential membership, all grouping
and aggregate references remain source-owned, and the selected expression has
exactly one derived duplicate-invariant aggregation. The Resolver carries the
selected expression's aggregation properties across its boundary; the Planner
combines them with relationship multiplicity and population-preservation
evidence and records the admission in the typed source node.

The canonical `COUNT(DISTINCT customer.customer_id)` scenario filters repeated
order matches by status and asserts the distinct customer result against every
real engine. Replacing it with `COUNT(customer.customer_id)` returns
`UNSUPPORTED_RELATIONSHIP_FANOUT`. Joined-side `IS NULL`, unfiltered fan-out,
multi-hop paths, joined-side grouping or expression references, multiple
aggregations, and unknown properties remain closed. Explain and semantic-plan
fingerprints include both multiplicity and the accepted aggregation evidence.

## Test and acceptance criteria

- every registered aggregate has asserted values for its duplicate class, rollup
  class, partial-state shape, merge operator, and finalizer;
- `COUNT` asserts `SUM` as its merge operator, not `COUNT`;
- `SUMIF` asserts duplicate-`Sensitive`, rollup-`Distributive`, a conditional-sum
  partial state, and `SUM` as its merge operator — its predicate is part of the
  partial state, not of the merge;
- `COUNT(DISTINCT x)` resolves duplicate-`Invariant` and rollup-`Holistic`,
  proving the two properties are independent;
- `AVG` is duplicate-`Sensitive` under per-row multiplicity, with a test whose
  multiplicities differ per row rather than being uniform;
- a rollup whose required partial state was not retained fails closed even when
  the aggregation is classified `Distributive` or `Algebraic`;
- an unknown or target-native function resolves `Unknown` and fails closed where
  a property is required, and is never reported as scalar-by-default;
- an opaque Ossie vendor extension asserting a safety property is ignored and
  resolves `Unknown`; only a registered interpreter under RFC-0021 contributes
  evidence;
- a holistic aggregate requested at a coarser grain than its inputs fails with a
  typed error rather than re-aggregating;
- derived arithmetic over shared-grain dependencies continues to compile
  unchanged;
- a duplicate-invariant aggregate over a fan-out join is accepted only with
  population-preservation proof, and still rejected without it;
- canonical scenarios covering each accepted relaxation carry real-engine result
  evidence, not compiler evidence alone;
- RFC-0015 semi-additive behavior is unchanged.

## Resolved implementation questions

**Error identity for the fail-closed paths.** Rollup refusals use
`INVALID_METRIC_ROLLUP`. Fan-out refusals use
`UNSUPPORTED_RELATIONSHIP_FANOUT`, map to `CHANGE_REQUEST`, and project to HTTP
422. Both codes are exhaustively registered.

**Population-preservation proof as a structure.** Source nodes carry typed,
relationship-aligned `PopulationPreservationEvidence`, including multiplicity,
four independent obligations, and optional fan-out admission evidence. Plan
validation, Explain, cloning, optimizer rewrites, and fingerprints all preserve
and validate it.

## Documentation updates

- `docs/specs/semantic/` — the aggregation property contract and its failure
  modes;
- `docs/specs/glossary.md` — the property vocabulary, including partial state and
  merge operator;
- `docs/design/semantic/pipeline.md` — where properties are derived and consumed,
  and the layer boundary between derived properties and the plan-owned decision;
- `tests/conformance/reference/baseline.go` — new adversarial fan-out and
  rollup cases in the executable reference baseline.
