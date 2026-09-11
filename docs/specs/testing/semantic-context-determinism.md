# Semantic Context Determinism

This specification defines Phase F3 correctness requirements for the bounded Agent semantic-context bundle exposed by `SemanticContextService`.

## Focus is mandatory

A semantic-context request MUST identify at least one metric or dimension. Metis MUST NOT turn an empty focus into a full-model dump.

The response MUST be derived from the current project/model SemanticManifest used by discovery and compatibility resolution.

## Relevant evidence only

Relationship evidence MUST be limited to relationships that occur in the compatibility paths returned for explicitly focused dimensions or for dimensions included in the requested compatible-dimension page.

Unrelated model relationships MUST NOT be included merely because they exist in the same semantic model.

## Deterministic response

For an unchanged SemanticManifest and an identical request, repeated semantic-context calls MUST produce the same ordered semantic evidence, compatible-dimension items, relationship list, and pagination cursor.

Deterministic ordering is required for Agent consumption and conformance. Map iteration order or other runtime-local ordering MUST NOT leak into observable responses.

## Bounded compatible-dimension pages

Compatible-dimension pages are explicitly bounded. The current service default is 20 items and the hard maximum is 100 items per request.

Requests above the maximum MUST fail with `INVALID_QUERY`; Metis MUST NOT silently widen the bound.

Cursors are opaque service values. For the same snapshot and page boundary, the emitted cursor MUST be stable and resume after the same qualified dimension identity. A cursor MUST fail closed when reused with a different semantic anchor or normalized search.

## Shared semantic truth

Compatibility evidence MUST continue to use `DiscoveryService` metric-source and relationship-path resolution. Semantic context MUST NOT implement a second relationship graph or infer compatibility from search ranking.

## Executable evidence

Phase F3 behavior is frozen by `app/service/semantic/semantic_context_determinism_test.go` together with the existing semantic-context pagination and compatibility tests.
