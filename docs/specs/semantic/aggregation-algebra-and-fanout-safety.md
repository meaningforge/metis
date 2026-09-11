# Aggregation Algebra and Fan-out Safety

## Derived aggregation properties

Metis derives duplicate sensitivity and rollup algebra from the SemanticManifest-bound,
typed metric expression. A model declaration or opaque vendor extension MUST
NOT assert either safety property. Unknown functions and expressions whose
property cannot be derived remain `UNKNOWN` and fail closed wherever the
planner requires the property.

For an otherwise unknown target-native aggregation, a registered RFC-0021
interpreter MAY contribute typed aggregation-algebra evidence only when its
semantic-critical capability resolves for the selected engine and dialect.
Resolver validates that evidence and attaches its interpreter identity to the
selected expression. An unregistered interpreter, a capability registered for
another target, or an opaque payload with equivalent-looking fields contributes
no property.

Duplicate invariance means invariance under arbitrary per-row multiplicity,
not merely uniform replication. `MIN`, `MAX`, and `COUNT(DISTINCT ...)` are
duplicate-invariant. `SUM`, `COUNT`, `AVG`, and conditional sums are duplicate-
sensitive.

A source aggregate may be rolled up only when its plan-owned `RollupContract`
proves one aggregation call, distributive algebra, and a retained one-component
partial state with identity finalization. The merge operator is derived from
the aggregation: notably, `COUNT` partial values merge with `SUM`. Unsupported
rollups fail with `INVALID_METRIC_ROLLUP`.

## Population-preservation evidence

Duplicate invariance alone MUST NOT authorize a fan-out traversal. Each source
aggregate relationship path carries typed `PopulationPreservationEvidence`
covering four independent obligations:

- `join_rows` — the relationship operation cannot remove source-population rows;
- `filter_placement` — filters do not make the aggregate depend on the joined side;
- `grouping` — grouping does not make the aggregate population joined-side dependent;
- `expression_references` — the aggregate expression reads only the proven population side.

Every obligation records `PROVEN` or `UNPROVEN` plus a closed planner-derived
proof identity. Evidence is population-preserving only when all four distinct
obligations are present and `PROVEN`. Semantic-plan validation rejects missing,
duplicate, unknown, mismatched, or join-misaligned evidence. Explain exposes the
same node-owned evidence and its derived `preserved` result.

Ordinary Ossie relationships lower to inner joins and do not declare
referential totality. Consequently, `join_rows` is normally
`UNPROVEN / inner_join_totality_unproven`, even when the target key is unique.
Target uniqueness proves only that a traversal cannot duplicate a source row;
it does not prove that every source row has a match.

Metis admits one deliberately narrow fan-out shape. A single relationship may
fan out when all of the following are planner-proven:

- at least one filter on the joined dataset is null-rejecting, so the requested
  population is exactly the source rows having a matching joined row;
- every other filter is owned by the source or that same joined dataset;
- grouping and aggregate expression references are owned by the source dataset;
- the selected expression contains exactly one aggregation and its derived
  duplicate sensitivity is `INVARIANT`.

For this shape, the joined filter proves both `join_rows` and
`filter_placement` with `joined_filter_defines_membership`. Arbitrary match
multiplicity cannot change the aggregate, while unmatched source rows are
intentionally outside the filter-defined population. `IS NULL` is not
null-rejecting. Unfiltered traversals, multiple joins, joined-side grouping or
expression references, and duplicate-sensitive or unknown aggregations remain
unproven and fail closed.

The evidence records relationship multiplicity as `AT_MOST_ONE` or
`MAY_FAN_OUT`. An admitted `MAY_FAN_OUT` relationship also records the selected
aggregation and its `INVARIANT` duplicate-sensitivity proof. Explain exposes
that admission evidence and semantic-plan fingerprints cover it.

Population-preservation evidence is semantic correctness state. It contributes
to the semantic-plan fingerprint and survives cloning and optimizer join
deduplication/pruning in lockstep with the relationship it describes.

## Attribution decomposition use

Metric change attribution consumes these derived properties; it does not add a
parallel algebra. Additive decomposition requires a distributive aggregate or a
supported linear derived metric plus governed identity fill at the exact
decomposition grain. Ratio decomposition requires an explicit division whose
ordered numerator and denominator dependencies are both independently proven
additive over the same population. Source `AVG`, distinct count, unknown
aggregation, nonlinear composition, and unsafe relationship traversal fail
closed. See
[`metric-change-attribution.md`](metric-change-attribution.md) for alignment and
reconciliation requirements.

## Failure discipline

No missing proof may be converted into a positive claim. SQL rendering,
physical-engine behavior, model-authored extensions, and target-key uniqueness
alone cannot manufacture population preservation. A rejected traversal returns
`UNSUPPORTED_RELATIONSHIP_FANOUT` with `CHANGE_REQUEST`; its details identify
the duplicate sensitivity, whether population preservation was proven, and the
remaining unproven obligations. Every newly accepted shape requires canonical
real-engine result evidence.
