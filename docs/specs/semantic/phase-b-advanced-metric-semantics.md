# Phase B advanced metric semantics

This specification makes the accepted Phase B proposals normative for Metis planning and lowering.

## Time offset

Time offset is a semantic coordinate transformation. The required order is input metric semantics, required grain/alignment, offset, query output time constraint, then final composition. Metric-definition filters remain on the input side of the offset. Query output time constraints remain on the output side. Optimizers must not move predicates across this boundary unless equivalence is proven.

## Fill policy

Fill applies to an aligned metric value before derived metric composition. Phase B supports `none` and numeric `zero`. Fill never applies to dimension/group keys and must not be lifted from an input metric to a final derived expression. `zero` requires a numeric-compatible metric output type.

## Semi-additive rollup

A semi-additive metric preserves its non-additive-dimension selector semantics at every valid rollup. Ordered-value metrics select by their ordering and tie-break contract before aggregation across remaining additive dimensions when operation order can affect the result.

A scalar semi-additive intermediate that has discarded selector state is non-composable across its non-additive dimension. Such a rollup must fail closed. Stateful intermediates may compose only when they retain the value, ordering key, and required deterministic tie-break state.

## Ossie interoperability

Metis prefers upstream Ossie fields whenever equivalent standard semantics exist. For the currently supported Ossie schema, incubating Metis semantics use the standard `custom_extensions` envelope with `vendor_name: METIS` and a typed JSON payload in `data`. Metis validates that payload into stable typed structures and preserves unrelated extensions. This is an interoperability-preserving incubation surface, not a fork of the Ossie core schema.

If upstream Ossie adds equivalent native fields or a structured extension payload, Metis should migrate to the standard representation with an explicit compatibility window for existing models.

Planner-derived properties such as semi-additive composability remain internal unless an upstream standard requires user-authored representation.

## References

- [0013 — Time Offset Alignment Semantics](../../proposals/semantic/0013-time-offset-alignment.md)
- [0014 — Metric Fill Policy](../../proposals/semantic/0014-metric-fill-policy.md)
- [0015 — Semi-Additive Rollup and Composability](../../proposals/semantic/0015-semi-additive-rollup-composability.md)
