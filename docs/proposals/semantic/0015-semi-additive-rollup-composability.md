# RFC-0015: Semi-Additive Rollup and Composability

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-15
- **Last updated:** 2026-09-03
- **Scope:** Rollup semantics for semi-additive metrics and planner composability
- **Supersedes:** None

## Decision

A semi-additive metric is not an ordinary aggregate that may be re-aggregated across every dimension. Metis MUST preserve the declared non-additive dimension semantics during rollup.

For an ordered-value metric such as end-of-period balance, the canonical logical order is:

1. partition by the requested additive grain;
2. select the ordered value along the non-additive dimension using the metric's selector (`first` or `last`) and deterministic tie-break contract;
3. aggregate across dimensions that remain additive;
4. expose the resulting metric value at the requested output grain.

For example, monthly balance MUST NOT be computed as `SUM(daily_balance)` when time is the non-additive dimension. The monthly value must be selected from the underlying ordered observations according to the metric contract.

## Composability

A previously materialized or intermediate semi-additive result may be rolled up again only if that intermediate preserves enough information to reproduce the selector semantics at the coarser grain.

At minimum, composable ordered-value state requires the value plus the ordering key and any deterministic tie-break key needed by the selector. A scalar value that has discarded this state is non-composable across the non-additive dimension.

If a requested rollup would require information that the intermediate result no longer carries, Metis MUST fail closed rather than apply a normal aggregate to the scalar result.

## Additive dimensions

Dimensions not declared non-additive retain their declared additive behavior. Selection along the non-additive dimension occurs before aggregation across remaining additive dimensions whenever changing that order could change the result.

The planner MAY reorder operations only when equivalence is proven for the specific metric contract.

## Null and tie-break semantics

The selector's existing null-skip/null-preserve behavior and deterministic tie-break contract remain authoritative during every valid rollup. A rollup MUST NOT replace ordered-value semantics with `MIN`, `MAX`, or another numeric aggregate merely because the final value is scalar.

## Planner contract

Metis derives composability from the semantic plan and retained state. Users do not need to declare `composable: true` or `false` in YAML.

The planner MUST distinguish:

- composable stateful semi-additive intermediates;
- terminal scalar semi-additive outputs;
- ordinary additive aggregates.

Crossing from a terminal scalar output to a coarser grain along its non-additive dimension without sufficient retained state is a semantic planning error.

## Ossie compatibility

Metis MUST consume Ossie-native semi-additive/non-additive-dimension semantics when available. Missing upstream fields MAY incubate under a typed `custom_extensions.metis.semi_additive` declaration.

Planner composability is derived execution semantics and SHOULD remain internal rather than becoming a user-authored Ossie extension unless an upstream standard explicitly requires such a declaration.
