# Core RFC: Bounded Boolean Filter Predicates

- **Status:** Draft — number assigned during review
- **Owners:** Metis Core maintainers
- **Created:** 2026-10-09
- **Last updated:** 2026-10-09
- **Scope:** Query-owned, same-stage dimension predicates across Core transports
- **Supersedes:** None

## Summary

Add a bounded, typed AND / OR / NOT tree that reuses existing filter leaves.
Keep the legacy `filters` array conjunctive and unchanged. The first version
accepts only dimension conditions that can be proven to evaluate on one input
population at the same pre-aggregation stage. It neither accepts arbitrary SQL
nor changes fanout admission, metric aggregation, row policy, or time semantics.

This is a proposal, not an implemented query contract. The numeric precision
guard merged in PR #31 remains authoritative for every new numeric leaf.

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

Propose an optional `predicate` on ordinary SemanticQuery and the corresponding
Agent compile/query-metrics inputs. Example (canonical refs abbreviated):

```json
{
  "metrics": [{"name": "revenue"}],
  "predicate": {
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

Omission of `predicate` means no new restriction. Explicit null, empty groups,
unknown kinds/fields, duplicate keys, ambiguous payloads, nested operand arrays,
and cycles in programmatically supplied trees fail validation. There are no
raw SQL, identifier-expression, arbitrary function, TRUE/FALSE literal nodes,
or user-selected stage fields. String operands remain values, never SQL.

Proposed operation-wide limits: depth 8 (root depth 1), 128 nodes, 64 leaves,
256 scalar operand elements and 64 KiB scalar bytes, including legacy leaves.
These are new-query limits; legacy-only requests retain existing limits.
Count before semantic resolution, rational numeric parsing, or planning; fail
instead of truncating. Transport schemas explain the grammar, but authoritative
Core validation also protects direct Go embedders. Scalars retain their current
types and numeric precision rules. Count typed scalar byte representations, not
only JSON punctuation or the number of leaf objects.

### Legacy normalization and stage admission

For a legacy-only request, preserve current resolution and placement exactly.
For a request with both fields, semantic intent is:

`AND(legacy filters, predicate)`.

Do not insert every legacy leaf into the new tree and then reject previously
valid metric filters. Normalize legacy conjuncts and the new tree into one
query-owned predicate representation, retaining the existing proven stage for
each independent legacy conjunct. A legacy post-aggregate metric conjunct may
coexist with the new pre-aggregate tree; it remains at its original stage.
It cannot become an OR/NOT child inside the new tree.

For the new tree, resolve **every** leaf before deciding admission. References,
field access, required-column closure, datatype/operator checks, join paths and
evaluation coordinates are determined through existing canonical resolution.
Branches cannot hide a denied or unsupported field just because another branch
would be true. The whole tree is admitted only if all leaves have one compatible
row population, relation input, grain and evaluation coordinate. References on
an already-safe many-to-one joined input may be allowed only when the existing
join/null-preservation and population proofs apply to the entire tree. Otherwise
reject; do not union independently compiled scans or broaden fanout rules.

V1 rejects metric leaves (including aggregate aliases), computed window-result
leaves, mixed stages, multi-root populations and unproved placement. A temporal
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
| query | Tagged grammar, bounds, legacy normalization contract, shared numeric decoding |
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
keys must all be updated together. New-tree requests may create new identities;
legacy-only requests keep their SQL, bind order, scope and existing fingerprints.
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

### Old-server safety is a release prerequisite

Current SemanticQuery decoding ignores many unknown keys. An old server might
silently ignore `predicate` and execute a broader query. JSON field addition
alone is therefore **not** a safe rollout mechanism.

Clients must not send the new shape until the targeted server/deployment supports
the versioned predicate contract. MCP's advertised input schema is necessary but
not sufficient for arbitrary REST deployments or mixed old/new backends. The
implementation must ship a fail-closed version/capability admission mechanism,
and its older-server acceptance test must prove zero query execution, not merely
that new-server parsing works. No fallback strips the predicate or splits it into
multiple requests. Supporting fleets must be homogeneous or route to a pinned
compatible generation; rollback removes feature advertisement before clients
can reach an old generation.

**Review-blocking decision:** choose versioned REST request admission versus an
explicit Core feature-negotiation contract, including embedder and CLI behavior.
This RFC deliberately does not invent an already-existing capability endpoint.
No API field implementation or RFC acceptance proceeds without resolving this
decision and demonstrating failure-closed behavior for older servers.

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
- Replacing legacy filters outright: unnecessary migration and query drift;
  additive normalization preserves old behavior.

## Rollout and migration

1. Review grammar, bounds, temporal admission, identities and old-server safety;
   settle version admission and assign an RFC number.
2. Implement shared types/resolution and immutable staged tree with explicit
   rejection paths, then SQLPlan/renderers and transport mapping as one bounded
   capability. Do not expose partially wired endpoints.
3. Run shared compile, policy and result acceptance. Advertise the feature only
   after all supported targets meet its contract. Legacy-only requests remain
   available and unchanged throughout.

The later relation-existence RFC remains independent. No SUM fanout exception,
AVG state merge, calendar rewrite, benchmark platform, or Cloud product work is
hidden in this rollout. Draft publication is not approval or implementation.

## Test and acceptance criteria

Reuse existing canonical fixtures and production Runner/Driver; add independent
oracles only where necessary. A target's absence is NOT_EXECUTED, never PASS.

- Overlapping population fixture: APAC/enterprise order 100, APAC/standard 50,
  EU/enterprise 70, EU/standard 30; all paid. OR SUM = 220, COUNT = 3, AVG =
  220/3 (compare using the existing numeric contract), not split-query SUM 320.
  Add an unpaid APAC/enterprise 500; the outer status AND still yields 220.
- NULL truth table: x NULL / target / other. `NOT(x = target)` selects only
  other; `x IS NULL OR x = target` selects NULL and target. Cover null-containing
  IN/NOT IN and nested NOT without rewriting UNKNOWN into a boolean value.
- Mandatory tenant policy: an otherwise matching high-value different-tenant
  row contributes nothing under OR/NOT. Denied leaves fail even in seemingly
  inactive branches; no credentials or Runner calls on denial.
- Legacy-only corpus unchanged; mixed legacy conjunct plus new input tree keeps
  original metric/time stage. Metric OR dimension, temporal output-range OR row
  predicate, ambiguous joined-population and unsupported fanout trees reject.
- Identical clones have identical identity; different AND/OR/NOT shapes, policy
  scopes or stage coordinates cannot fuse. Optimizer on/off results agree with
  the independent oracle and preserve the predicate tree's placement.
- Grammar/boundary tests at and beyond each bound, duplicate keys, strict new
  leaf fields, illegal payloads and programmatic cycles. All numeric leaves,
  including below NOT/OR and through MCP raw bytes, reuse precision rejection.
- Actual REST/MCP/CLI mapping plus older-server rejection. Do not label a local
  decoder-only test as end-to-end compatibility proof.
- Same logical result cases on DuckDB, Doris and ClickHouse; parameter bindings,
  parenthesization and NOT/NULL semantics are independently inspected. Explicit
  real-engine execution evidence is required before marking Implemented.

All criteria above are pending. This draft adds no tests or runtime behavior.

## Documentation updates

On implementation, update the current agent-query contract, semantic query
grammar, numeric-filter limits, policy integration, SQLPlan projections,
optimizer identity contracts and CLI examples. Keep this draft in proposals;
do not describe the feature as currently supported in README or specifications.

Related current contracts: [Agent query](../../specs/semantic/agent-query-contract.md),
[data policy](../../specs/operations/data-access-policy.md),
[fanout safety](../../specs/semantic/aggregation-algebra-and-fanout-safety.md),
[logical stages](0029-semantic-logical-stage-model.md),
[typed SQLPlan](../sql/0038-typed-sql-plan.md).
