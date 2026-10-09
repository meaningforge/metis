# RFC-0092: Safe Relationship Existence Filters

- **Status:** Implemented
- **Owners:** Metis Core maintainers
- **Created:** 2026-10-09
- **Last updated:** 2026-10-09
- **Scope:** Query-owned, one-hop relationship membership filters in Metis Core
- **Supersedes:** None

## Summary

Add an explicit relationship-existence predicate for queries in which a related
detail dataset determines membership in a source population but must not change
the source grain. The first version supports one declared relationship from a
source to a possibly-many target, a positive `EXISTS` test, and a bounded
target-side dimension predicate. Metrics, grouping and ordering remain on one
source dataset.

The planner represents membership separately from ordinary joins and lowers it
through SemanticPlan and SQLPlan to a correlated `EXISTS` subquery. It does not
relax the existing fanout admission rule, inject `DISTINCT` around measure
values, or reinterpret an unsafe join as safe. Source and target data policies
remain mandatory and are applied at their owned relation inputs.

## Motivation

Consider the question “what is the total value of orders that contain a target
product category?” Given these rows:

| Order | Amount | Matching detail rows |
| --- | ---: | ---: |
| A | 100 | 2 |
| B | 100 | 1 |
| C | 50 | 0 |

the correct total is 200. Joining details before `SUM(orders.amount)` produces
300. `SUM(DISTINCT orders.amount)` produces 100 because two different orders
have the same amount. The duplicate identity is the order key, not the measure
value.

The current fanout guard correctly rejects duplicate-sensitive aggregates such
as SUM, COUNT and AVG when a one-to-many traversal can repeat source rows. Its
limited admission of proven duplicate-invariant aggregates is not a solution
for source-population membership. Removing or weakening that guard would make
valid SQL return incorrect analytical results.

The intended relational operation is a semijoin: retain one source row when at
least one related target row satisfies a condition. An explicit query intent is
needed so the planner can prove that the related dataset changes membership but
not multiplicity.

## Design

### Public query shape

Extend the tagged predicate grammar introduced by RFC-0091 with one `exists`
node. The canonical request is:

```json
{
  "model": "commerce",
  "metrics": [{"name": "order_revenue"}],
  "dimensions": [{"name": "order_region"}],
  "filters": {
    "kind": "and",
    "children": [
      {
        "kind": "filter",
        "filter": {
          "field": "order_status",
          "operator": "eq",
          "value": "paid"
        }
      },
      {
        "kind": "exists",
        "relationship": "orders_to_items",
        "where": {
          "kind": "filter",
          "filter": {
            "field": "item_category",
            "operator": "eq",
            "value": "target"
          }
        }
      }
    ]
  }
}
```

`exists` is a distinct tagged node, not a Filter operator and not a raw SQL
fragment:

| Field | Requirement |
| --- | --- |
| `kind` | Exactly `exists` |
| `relationship` | Required declared relationship name |
| `where` | Required bounded predicate over target-side dimension leaves |
| Other fields | Rejected |

An existence node may be the sole root predicate or a direct child of the root
AND. V1 rejects existence below OR, NOT, another existence node, or any nested
AND that prevents it from being extracted as an independent root conjunct.
This avoids claiming equivalence for expressions such as `source_condition OR
EXISTS(...)` before cross-population boolean staging is designed.

The target `where` tree may use filter, AND, OR and NOT under RFC-0091's SQL
three-valued logic. Every leaf must resolve to a non-metric field owned by the
relationship target dataset at its input-row stage. Aggregate aliases, metric
filters, time-window outputs, source-side fields, additional relationship
traversals and user-selected stage annotations are rejected.

The operation-wide RFC-0091 limits remain authoritative: depth 8, 128 nodes, 64
filter leaves, 256 scalar operand elements and 64 KiB of scalar data. The
existence node and its target tree count toward the same budget; it does not
receive a second allowance. Duplicate keys, unknown fields, unknown kinds,
ambiguous payloads and malformed target trees fail before semantic planning.

There is no `not_exists` spelling in V1. Wrapping an existence node in NOT is
also rejected. Absence membership has additional policy, NULL and optional-row
semantics and can be proposed separately with independent acceptance cases.

### Admission and relationship proof

The Resolver and Planner admit one existence node only when all of the following
are proven:

1. the query has one source evaluation population;
2. every selected metric expression, dimension and order field is owned by that
   source population or already valid without the existence relationship;
3. `relationship` identifies exactly one declared, ordinary relationship
   incident to the query source; traversal direction is recorded explicitly and
   the other endpoint owns every target predicate leaf;
4. the relationship has non-empty, aligned source and target key columns;
5. the target predicate is an input-row predicate on that target dataset;
6. both source and target required-field and policy closures are authorized;
7. no temporal, many-to-many, multi-hop, attribution or allocation semantics
   are required to interpret membership.

The relationship target is allowed to contain any number of matching rows for a
source key because EXISTS returns one boolean membership result. Target-key
uniqueness is neither required nor inferred. Source identity still comes from
the ordinary source plan and declared model keys; existence is not a repair for
an invalid source grain.

V1 supports at most one relationship-existence conjunct per query. Multiple
existence predicates, even on the same relationship, are rejected rather than
silently combined: “one row matching both conditions” and “one row for each
condition” are different meanings. A later RFC may introduce explicit
quantification for that distinction.

Ordinary joined-dimension queries and implicit fanout traversals keep their
existing rules. A query that attempts to group by a detail dimension while
aggregating a source measure still fails with
`UNSUPPORTED_RELATIONSHIP_FANOUT`; the caller must not obtain existence
semantics merely because a related filter is present.

### Logical and physical plans

SemanticPlan adds a typed relationship-membership predicate carrying:

- canonical relationship identity and direction;
- source and target dataset identities;
- ordered correlation key pairs;
- the resolved target predicate tree and its input-stage coordinate;
- required-field closure and non-sensitive admission evidence.

This predicate belongs to the source-aggregate input population but is not an
ordinary Join. SemanticPlan validation requires complete correlation evidence,
one target relation, a target-owned predicate and no selected output from the
target. Explain output may identify the relationship, datasets and membership
kind; it must not reveal operand values or policy predicates.

Conversion produces a typed SQLPlan existence predicate with an outer source
reference, one target TableSource, ordered equality correlations and the target
predicate. SQLPlan validation rejects missing aliases, empty correlations,
cross-target leaves, selected target columns and nested existence plans.

All built-in renderers emit the equivalent of:

```sql
SELECT SUM(o.amount)
FROM orders AS o
WHERE EXISTS (
  SELECT 1
  FROM order_items AS i
  WHERE i.order_id = o.order_id
    AND i.category = ?
)
```

Composite relationships use one equality per aligned key joined by AND. SQL
ordinary equality is intentional: NULL correlation keys do not match. Renderers
must not introduce null-safe equality, COALESCE, outer joins, `DISTINCT` measure
rewrites or target projections. Parameters follow deterministic tree traversal
order after mandatory relation constraints are installed.

An optimizer may later replace EXISTS with a proven equivalent semijoin, but V1
does not require or authorize that transformation. Optimizer-on and
optimizer-off plans must preserve the same source population and results.

### Policy and authorization

The effective source population is:

```text
source policy
AND source user predicates
AND EXISTS(target relation constrained by target policy AND target predicate)
```

Source policy stays on the source relation. Target policy is installed inside
the existence subquery before correlation and user filtering. Neither policy is
serialized into the user predicate tree, exposed through explain, nor made
negatable by user input.

Preflight collects relationship keys, every target predicate field and all
source/target policy dependencies before loading execution credentials or
opening a database connection. A denied target field or relationship fails even
though no target column is returned. A denied or malformed branch cannot be
hidden by target-side OR. Rejection causes zero credential, connection and
Runner calls and does not expose raw operands, policy values or backend causes.

If current policy adapters cannot produce a target-owned relation constraint at
the required scope, the query fails closed. Metis must not run an ungoverned
target subquery and must not approximate target policy at the outer source.

### Identity, cloning and diagnostics

Canonical identity includes the membership kind, relationship direction,
ordered key pairs, target dataset, target predicate shape, stage coordinate and
applicable policy scope. Clone, explain, projection, optimizer fusion and cache
keys must preserve those values without shared mutable slices.

Malformed structure returns `INVALID_QUERY`. Semantically unsupported placement
or more than one existence conjunct returns `UNSUPPORTED_QUERY_SHAPE` with
`CHANGE_REQUEST`. Existing unknown-reference, denied-field, invalid-value and
relationship-fanout errors retain their current codes. Diagnostics may identify
the failed node path and relationship but never include filter values or policy
contents.

### Responsibilities

| Layer | Responsibility |
| --- | --- |
| query | Strict `exists` grammar and shared operation-wide predicate limits |
| REST / MCP / CLI | Expose the same shape and preserve numeric tokens; no transport-specific membership language |
| Resolver | Bind the relationship and target leaves; close source, key, target and policy dependencies |
| Planner / SemanticPlan | Prove one-hop membership and source-grain preservation; keep membership separate from joins |
| Optimizer | Preserve correlation, target predicate, policy and source grain |
| planner/conversion / SQLPlan | Carry typed outer reference, target relation, key pairs and target predicate |
| Renderer | Emit correlated EXISTS with bound parameters and exact parenthesization |
| Runner / Driver | Execute the completed artifact without interpreting relationship semantics |

## Alternatives

- Permit SUM across the ordinary fanout join: duplicates source measures and is
  incorrect; rejected.
- Use `SUM(DISTINCT measure)`: merges different source entities that share a
  value; rejected.
- Join a `SELECT DISTINCT foreign_key` subquery: potentially equivalent for the
  bounded case, but introduces a second physical strategy and NULL/dedup proof;
  deferred while EXISTS is the canonical lowering.
- Infer membership from any detail-side filter: changes an ordinary join's
  meaning implicitly and makes query intent unstable; rejected.
- Accept raw SQL EXISTS text: bypasses resolution, policy, parameterization and
  plan identity; rejected.
- General semijoin/antijoin syntax, multiple relationships or multi-hop paths:
  useful later, but materially broader than the first correctness proof.
- Expose target dimensions in the result: changes output grain and is not an
  existence-only query; callers retain the existing fanout safety contract.

## Rollout and migration

1. Review this grammar, the one-hop proof, policy placement and plan ownership.
2. Implement the full Core path in one change: query types, transport schemas,
   Resolver, SemanticPlan, SQLPlan, renderers, callers, tests and documentation.
   Do not expose an `exists` node on any public transport until every built-in
   renderer and validation boundary is wired.
3. Run shared compiler/result acceptance and the explicit DuckDB, Doris and
   ClickHouse real-engine matrix before marking this RFC Implemented.

Metis is pre-stable. No legacy membership spelling, feature negotiation,
automatic downgrade or older-server compatibility adapter is required. Existing
filter/AND/OR/NOT requests retain their meaning; unknown `exists` requests sent
to an older revision may fail normally. Rollback reverts the public node and all
plan/rendering support together rather than leaving a partially accepted shape.

## Test and acceptance criteria

Use one canonical order/detail fixture and independent result oracles:

- orders A=100 and B=100 have respectively two and one matching details; order
  C=50 has none. EXISTS SUM is 200, COUNT is 2 and AVG is 100;
- adding duplicate matching details does not change any result;
- equal amounts on A and B remain separate source rows, proving that
  `SUM(DISTINCT amount)` was not used;
- removing all matching details for one order removes exactly that order;
- source filters combine by AND with membership and retain ordinary NULL logic;
- nested AND/OR/NOT inside target `where` preserves SQL UNKNOWN and counts a
  source row once when multiple target branches or rows match;
- source policy excludes an otherwise matching order and target policy can make
  an otherwise matching detail invisible; user predicates cannot bypass either;
- denied target fields, invalid relationships, target metrics, target output
  dimensions, nested/multiple existence, NOT EXISTS, multi-hop and temporal
  relationships reject before execution with the documented error class;
- the existing unsafe joined SUM/COUNT/AVG cases remain rejected and the
  duplicate-invariant admission cases remain unchanged;
- SemanticPlan/SQLPlan validation, clone, explain, identity, cache/fusion and
  optimizer differential tests cover correlation evidence and mutation safety;
- generated SQL contains a correlated EXISTS and no outer detail JOIN or
  measure DISTINCT repair; parameter order and composite correlations are
  inspected independently;
- REST, MCP and CLI accept the same JSON and reject unknown or partial shapes;
- the same logical fixture and manually calculated results pass through the
  production Runner/Driver on DuckDB, Doris and ClickHouse. An unavailable
  engine is NOT_EXECUTED, never PASS.

## Documentation updates

Implementation must update:

- `docs/specs/public-contract.md` for the new query node;
- `docs/specs/semantic/agent-query-contract.md` for Agent-visible grammar and
  failure boundaries;
- `docs/specs/semantic/aggregation-algebra-and-fanout-safety.md` to distinguish
  membership from ordinary fanout admission;
- `docs/specs/semantic/semantic-plan-node-model.md` for typed relationship
  membership evidence;
- README and executable request examples with one bounded EXISTS query;
- conformance scenario documentation and engine evidence for the new oracle.
