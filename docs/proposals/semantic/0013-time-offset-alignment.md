# RFC-0013: Time Offset Alignment Semantics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-15
- **Last updated:** 2026-09-03
- **Scope:** Semantic planning and lowering of time-offset metrics
- **Supersedes:** None

## Decision

Metis treats a time offset as a semantic time-coordinate transformation, not as a rewrite of the input `WHERE` predicate.

The normative evaluation order is:

1. resolve and evaluate the input metric with its metric-definition filters;
2. aggregate or align the input to the grain required by the offset contract;
3. shift the metric result onto the requested output time coordinate;
4. apply query output time constraints;
5. compose the shifted metric with other requested metrics or derived expressions.

A planner or optimizer MUST NOT move a query output time constraint ahead of the offset transformation when doing so changes the rows available to compute the shifted value.

## Offset domains

Metis distinguishes two domains:

- fixed/calendar offsets whose units have stable native semantics in the target dialect;
- custom-calendar offsets whose alignment is defined by the selected custom-calendar hierarchy and ordinal.

The two domains share the same semantic evaluation order but may lower differently.

## Alignment and missing coordinates

The requested output time coordinate is authoritative. If an offset source row is absent, the result at that output coordinate is missing/`NULL`; Metis MUST NOT silently drop the output coordinate merely because the shifted source row is absent when the surrounding query contract requires a dense coordinate set.

Time-spine or equivalent alignment may therefore be required before final metric composition.

## Filters

Metric-definition and other input-semantic filters execute before the offset. Query output time constraints execute after the offset. Other query predicates retain their normal ownership semantics and MUST NOT be pushed across the offset boundary unless equivalence is proven.

## Optimizer contract

The offset boundary is semantics-bearing. Optimizations may collapse or reorder physical nodes only when they preserve:

- input metric-definition filters;
- offset grain/alignment;
- output coordinate set;
- query output time constraints;
- null/missing-row behavior.

When equivalence is not proven, planning MUST fail closed rather than silently produce a differently scoped metric.

## Ossie compatibility

Metis MUST consume an Ossie-native representation when the upstream schema exposes equivalent offset semantics. Until then, offset declarations MAY be carried as a typed `custom_extensions.metis` structure. The extension is an incubation surface, not a fork of the Ossie core schema.

Planner-only properties such as whether a particular lowering is safe to compose are derived by Metis and are not required to appear in user YAML.
