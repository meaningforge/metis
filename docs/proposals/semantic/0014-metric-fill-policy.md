# RFC-0014: Metric Fill Policy

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-15
- **Last updated:** 2026-09-03
- **Scope:** Missing metric values after alignment and before metric composition
- **Supersedes:** None

## Decision

Fill is a metric output semantic contract. It is not a renderer convenience and MUST NOT be implemented as an arbitrary final-query `COALESCE`.

For Phase B, Metis defines two fill policies:

- `none`: preserve missing metric values as `NULL`;
- `zero`: replace a missing metric value with numeric zero.

A future literal fill policy requires an explicit contract for type compatibility and is outside this proposal.

## Evaluation order

The normative order is:

1. evaluate the metric and its input semantics;
2. establish the requested output coordinate set and align metric results onto it;
3. apply the metric's fill policy to that metric value;
4. evaluate derived metric expressions and final composition.

For a derived expression such as `A / B`, a fill policy on `A` applies to `A` before division. It MUST NOT implicitly fill `B` or the final derived result.

## Missing row versus NULL value

Metis distinguishes logical coordinate absence from a materialized row whose metric expression evaluates to `NULL`, but both may produce a missing metric value after alignment. A fill policy operates on the aligned metric value, not on dimensions or grouping keys.

The planner MUST preserve the requested coordinate set before applying fill. It MUST NOT use fill as a reason to invent dimension values.

## Type contract

`zero` is valid only for metrics whose output type can represent numeric zero. If type compatibility cannot be proven, semantic analysis MUST fail closed.

Fill does not change the declared metric type except for normal nullability refinement that can be proven by the planner/compiler contract.

## Optimizer contract

Fill is semantics-bearing and belongs to the metric boundary. An optimizer may push or fuse the physical expression only if it proves equivalence with respect to:

- coordinate alignment;
- metric ownership;
- nullability;
- derived-expression ordering.

It MUST NOT lift a per-input fill to a final derived expression.

## Ossie compatibility

Metis MUST prefer an Ossie-native fill declaration when the upstream schema exposes equivalent semantics. In the currently supported Ossie schema, `custom_extensions[].data` is a string, so an incubating Metis fill declaration MAY be encoded as a typed JSON payload inside that standard `data` field, for example `{"kind":"fill","policy":"zero"}` with `vendor_name: METIS`.

The JSON payload is an implementation of Ossie's existing extension envelope, not a fork or second semantic-model language. Metis MUST expose typed validation and stable Go structures for the payload, and MUST preserve unknown non-Metis extensions during round trip. If Ossie later gains an equivalent native fill field or a structured extension payload, Metis SHOULD migrate to it while retaining compatibility with the incubating representation for an explicit transition period.
