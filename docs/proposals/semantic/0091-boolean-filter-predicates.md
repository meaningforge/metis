# RFC-0091: Bounded Boolean Filter Predicates

- **Status:** Implemented
- **Owners:** Metis Core maintainers
- **Created:** 2026-10-09
- **Last updated:** 2026-10-09
- **Scope:** Query-owned, same-stage dimension predicates across Core transports
- **Supersedes:** None

## Summary

Add a bounded, typed AND / OR / NOT tree that reuses existing filter leaves.
Replace the current flat `filters` array with one predicate object under the
same field name; do not add a parallel `predicate` field or compatibility mode.
The project is pre-stable and can make this explicit breaking contract change.
The first version admits OR/NOT only over dimension conditions proven to share
one input population and pre-aggregation stage. Existing independently placed
metric/time conjuncts remain expressible through the same tree. It accepts no SQL
nor changes fanout admission, metric aggregation, row policy, or time semantics.

This contract is implemented across REST, MCP, the offline CLI, SemanticPlan,
SQLPlan, and all built-in renderers. The numeric precision guard merged in PR
#31's fail-closed boundary, now upgraded to exact typed numeric operands, remains
authoritative for every numeric leaf.

## Motivation

The current public query has `Filters []Filter`, with implicit AND. It cannot
express `(region = 'APAC' OR customer_tier = 'enterprise') AND status = 'paid'`.
Splitting this query and adding its results double-counts overlapping people.
`in` only addresses alternative values of one field, not cross-field OR.

Resolver currently classifies field and metric filters separately. Builder
owns their evaluation stages; SQLPlan predicates are currently flat records.
Relation policies are separately enforced on constrained source inputs. A
renderer-only OR addition would bypass these responsibilities and is rejected.

## Design

### Public shape and bounded grammar

Use optional `filters` with a tagged Predicate value on ordinary SemanticQuery
and the corresponding Agent compile/query-metrics and offline CLI inputs.
One field, one grammar and one semantic pipeline. A single condition is a filter
node; a flat conjunction is an AND node. Example (canonical refs abbreviated):

```json
{
  "metrics": [{"name": "revenue"}],
  "filters": {
    "kind": "and",
    "children": [
      {
        "kind": "or",
        "children": [
          {"kind": "filter", "filter": {"field": "region", "operator": "eq", "value": "APAC"}},
          {"kind": "filter", "filter": {"field": "customer_tier", "operator": "eq", "value": "enterprise"}}
        ]
      },
      {"kind": "filter", "filter": {"field": "status", "operator": "eq", "value": "paid"}}
    ]
  }
}
```

Each node is a tagged sum, not independent optional booleans:

| Kind | Required payload | Forbidden payload |
| --- | --- | --- |
| filter | One existing Filter leaf | children, child |
| and / or | 2–32 children | filter, child |
| not | Exactly one child | filter, children |

Omission of `filters` means no user restriction. Explicit null, the former array
shape, empty groups,
unknown kinds/fields, duplicate keys, ambiguous payloads, nested operand arrays,
and cycles in programmatically supplied trees fail validation. There are no
raw SQL, identifier-expression, arbitrary function, TRUE/FALSE literal nodes,
or user-selected stage fields. String operands remain values, never SQL.

Operation-wide limits are depth 8 (root depth 1), 128 nodes, 64 leaves,
256 scalar operand elements and 64 KiB scalar bytes across the entire tree.
The same limits apply to flat conjunctions and nested boolean requests.
Count before semantic resolution, rational numeric parsing, or planning; fail
instead of truncating. Transport schemas explain the grammar, but authoritative
Core validation also protects direct Go embedders. Scalars retain their current
types and numeric precision rules. Count typed scalar byte representations, not
only JSON punctuation or the number of leaf objects.

### Unified normalization and stage admission

Use one query-owned tree for both flat AND and nested boolean conditions.
Resolve **every** leaf before deciding admission. References,
field access, required-column closure, datatype/operator checks, join paths and
evaluation coordinates are determined through existing canonical resolution.
Branches cannot hide a denied or unsupported field just because another branch
would be true. Each indivisible OR/NOT subtree is admitted only if all its leaves have one compatible
row population, relation input, grain and evaluation coordinate. References on
an already-safe many-to-one joined input may be allowed only when the existing
join/null-preservation and population proofs apply to the entire subtree. Otherwise
reject; do not union independently compiled scans or broaden fanout rules.

At the root, AND may combine independently placed predicates under existing
semantics: for example an input-row dimension condition and a post-aggregation
metric condition. Extract conjuncts only through AND, preserving deterministic
traversal; never split inside OR or NOT. A single metric filter leaf retains its
existing supported operators and post-evaluation placement. This preserves
analytical capability, not the previous wire format.

Every OR/NOT subtree must have a whole-tree common-input-stage proof. V1 rejects
metric leaves (including aggregate aliases) inside OR/NOT, computed window-result
leaves, mixed stages inside indivisible subtrees, multi-root populations and
unproved placement. A temporal
field is not automatically a scan predicate: an output time range for offset,
rolling or calendar evaluation must retain its existing coordinate semantics.
Until whole-tree equivalence is proved, these temporal combinations are rejected
using the existing time-filter boundary rather than extracting range leaves out
of OR/NOT. Ordinary temporal comparisons are eligible only where they already
have a proven common input-stage interpretation.

`distinct_values` may use admitted dimension trees at its existing stage. V1
does not add this shape to attribution/comparison workflows, authored metric
definition filters, or adapter-owned row policies. Those are separate contracts.

### SQL three-valued logic

Use SQL TRUE / FALSE / UNKNOWN; WHERE admits only TRUE. Do not coalesce UNKNOWN
to FALSE inside a tree. In particular `NOT(x = value)` must preserve UNKNOWN for
NULL x; NULL membership, `not_in`, and null tests retain existing leaf semantics.
Do not rewrite NOT into comparator complements without a proven null-equivalence
rule. OR evaluates membership once, not as additive aggregation of branches.

### Policy remains mandatory

Effective population is governed by mandatory relation constraints **and** the
user condition. A user cannot supply, replace, negate, or OR a policy subtree.
Policy constraints stay on every owned relation input and continue lowering as
FilteredTableSource before joins. This is not an instruction to move policies
into outer WHERE: doing so changes outer-join and preserved-row semantics.

Operation-wide required-field collection includes all boolean leaves plus policy
dependencies. Denied-field checks, generation ownership, preflight atomicity and
scope-aware fusion from the current data-access-policy specification continue
unchanged. Compiler/renderer/Runner never receive a Principal or policy adapter.

### Responsibilities and lowering

| Layer | Responsibility |
| --- | --- |
| query | One tagged grammar, bounds, AND normalization, shared numeric decoding |
| REST / MCP / CLI | Map the same public intent; preserve raw numeric tokens before coercion; no independent boolean language |
| Resolver | Bind all leaves and close field dependencies; classify semantic targets and placement requirements |
| Planner / SemanticPlan | Own immutable predicate tree and whole-tree stage/population proof; keep policy and user predicates separate |
| Optimizer | Preserve stages, boolean grouping and policy constraints; transform only with proven equivalence |
| planner/conversion → SQLPlan | Lower resolved leaves plus explicit logical nodes; carry tree through copies, validation and canonical projection |
| Renderer | Parenthesized AND/OR/NOT with bound parameters; reject unsupported nodes, never concatenate user SQL |
| Runner / Driver | Execute completed artifact without reinterpretation |

Add explicit logical nodes rather than arbitrary operator strings in SQLPlan.
All three renderers consume the same representation. Parenthesize every logical
group so precedence cannot change intent. Preserve deterministic traversal and
binding order; identities must include the entire predicate shape, resolved
sources, stages, operand types/values and mandatory policy scope.

Canonical hashes, explain projections, cloning, relation identities and fusion
keys must all be updated together. Flat conjunctions in the new grammar must
preserve existing results and stage placement; representation fingerprints may
change through a reviewed migration, never through regenerated result oracles.
AND reassociation must preserve deterministic leaf order and NULL semantics.
No flattening across OR/NOT, exponential DNF/CNF conversion, branch-union lowering,
or pushdown of one OR child independently of its parent is allowed in V1.

### Errors and diagnostics

Malformed, over-limit or unprovably staged predicates fail with INVALID_QUERY
and CHANGE_REQUEST; already-defined reference/type/access/time errors retain
their public classifications. Diagnostics may identify bounded node paths and
the failed structural/stage rule, but never raw operands, policy predicates,
Principal fields or backend causes. Explain may describe user-tree structure
under its existing disclosure contract; policy-only details remain redacted.
Failure must occur before credentials, connections or execution.

### Strict input is correctness, not historical compatibility

Do not introduce versioned parallel endpoints, capability negotiation, legacy
array adapters, automatic downgrade or an older-server support matrix. Publish
the breaking query change and update first-party callers together. Independent
consumers must update to the documented contract; compatibility with previous
pre-stable revisions is not promised.

The shared request decoder must reject unknown query/condition fields, malformed
trees and unsupported semantics. A misspelled `filters` or an invented
`predicate` must not disappear during unmarshalling and produce an unrestricted
query. Known transport envelope fields remain validated by their own types; no
generic bag of SQL or filter extensions is added. MCP validation must inspect
original bytes before SDK coercion, just as the numeric guard requires.

No fallback drops a condition, accepts the old array, or splits OR into multiple
requests. A rejected request causes zero credential/connection/Runner calls.
This fail-closed behavior is required even for the first public release, without
building a compatibility platform around it.

## Alternatives

- Raw SQL predicates: bypass typed resolution, policy and staging; rejected.
- Split OR into queries and sum results: overlapping populations and DISTINCT
  semantics make this incorrect; rejected.
- Arbitrary cross-stage trees: require new population/aggregation semantics;
  deferred, not approximately lowered.
- Boolean trees for row-policy adapters: separate authority and population
  contract; not required for this user-query feature.
- DNF rewriting or renderer-only strings: expansion and missing stage/identity
  evidence; not selected.
- Maintaining both an array and a tree field: duplicate contracts, admission and
  migration rules without a stable-release obligation; rejected. One field is
  simpler, while equivalent conjunctions still require result/stage parity.

## Rollout and migration

1. Review the single grammar, bounds, stage admission and identities; assign an
   RFC number. No feature-negotiation decision blocks the implementation.
2. Implement shared types/resolution and immutable staged tree with explicit
   rejection paths, then SQLPlan/renderers and transport mapping as one bounded
   capability. Do not expose partially wired endpoints.
3. Update first-party callers, request fixtures, documentation and examples to
   the same `filters` object in that implementation change. Run shared compile,
   policy and result acceptance before declaring it supported. Document the
   breaking pre-stable API/Go type change; retain no legacy parser or aliases.

Go callers construct the same Predicate type, not an independently interpreted
filter slice. The offline CLI uses the same leaf operator spelling as REST/MCP
inside this tree. Existing leaf operator meaning, numeric safeguards and policy
authority remain unchanged; changing the wire shape is not permission to change
the mathematical results of an equivalent conjunction. Rollback restores the
matching code, callers and examples together, not a dual-stack runtime.

The later relation-existence RFC remains independent. No SUM fanout exception,
AVG state merge, calendar rewrite, benchmark platform, or Cloud product work is
hidden in this rollout.

## Test and acceptance criteria

Reuse existing canonical fixtures and production Runner/Driver; add independent
oracles only where necessary. A target's absence is NOT_EXECUTED, never PASS.

- `boolean_filter_overlap_counts_once`: the canonical commerce fixture applies
  `(region = APAC OR segment = enterprise) AND status = paid`; expected revenue
  is 300, order count is 2 and average order amount is 150. The overlapping row
  is counted once rather than once per matching branch.
- NULL truth table: x NULL / target / other. `NOT(x = target)` selects only
  other; `x IS NULL OR x = target` selects NULL and target. Cover null-containing
  IN/NOT IN and nested NOT without rewriting UNKNOWN into a boolean value.
- Mandatory tenant policy: an otherwise matching high-value different-tenant
  row contributes nothing under OR/NOT. Denied leaves fail even in seemingly
  inactive branches; no credentials or Runner calls on denial.
- Migrate existing conjunction fixtures to AND trees without changing their
  independent expected results. Root AND containing row, metric and eligible
  time conjuncts keeps original stages. Metric OR dimension, temporal output-range OR row
  predicate, ambiguous joined-population and unsupported fanout trees reject.
- Identical clones have identical identity; different AND/OR/NOT shapes, policy
  scopes or stage coordinates cannot fuse. Optimizer on/off results agree with
  the independent oracle and preserve the predicate tree's placement.
- Grammar/boundary tests at and beyond each bound, duplicate keys, strict new
  leaf fields, illegal payloads and programmatic cycles. All numeric leaves,
  including below NOT/OR and through MCP raw bytes, reuse precision rejection.
- Actual REST/MCP/CLI mapping under the single shape. Unknown query/condition
  fields, the retired array shape and unsupported trees fail before execution.
  No older-server qualification or negotiated compatibility is required.
- Same logical result cases on DuckDB, Doris and ClickHouse; parameter bindings,
  parenthesization and NOT/NULL semantics are independently inspected. Explicit
  real-engine execution evidence is required before marking Implemented.

The shared result corpus includes `boolean_filter_overlap_counts_once`,
`boolean_filter_with_metric_filter`, `boolean_filter_with_derived_metric`,
`boolean_filter_not_preserves_unknown`, and `boolean_filter_null_or_value`.
The same logical fixtures and hand-authored results are consumed by DuckDB,
Doris, and ClickHouse execution targets.

## Documentation updates

The current agent-query contract, semantic query grammar, numeric-filter limits,
policy integration, SQLPlan projections, optimizer identity contracts and CLI
examples are updated with the implemented contract.

Related current contracts: [Agent query](../../specs/semantic/agent-query-contract.md),
[data policy](../../specs/operations/data-access-policy.md),
[fanout safety](../../specs/semantic/aggregation-algebra-and-fanout-safety.md),
[logical stages](0029-semantic-logical-stage-model.md),
[typed SQLPlan](../sql/0038-typed-sql-plan.md).
