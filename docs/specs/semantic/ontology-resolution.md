# Ontology discovery and resolution evidence

The shared DiscoveryService exposes `SearchOntologyConcepts` and
`ResolveOntologyConcept`. Both require Project `discover` authorization and
use one immutable Project generation. Neither constructs a SemanticQuery,
selects a metric, calls a Renderer, or executes a query.

REST exposes POST `/v1/projects/:project/ontology/search` and POST
`/v1/projects/:project/ontology/resolve`. Production MCP exposes
`search_ontology_concepts` and `resolve_ontology_concept`, with the same service
DTOs, errors and semantics. MCP inherits normal active Project context; REST
uses the path Project. A request cannot supply a Principal or policy.

## Discovery

Concept search accepts `project_id`, required `query`, optional `limit` and
`cursor`. Query text is limited to 1,024 bytes, cursors to 4,096 bytes; limits
default to 20 and cannot exceed 100. Blank queries are rejected.
Encoded responses are capped at 64 KiB. Pages shrink without changing ambiguity
state; an indivisible oversized concept or evidence item fails explicitly rather
than silently dropping provenance. This is independent of the compact metric
list's smaller presentation budget.

Search uses concept name, description, declared type and extends names only.
Raw mapping expressions and mapping-derived dataset/field targets MUST NOT
participate in either generic or dedicated Agent discovery indexes, matching,
ranking, reasons, pagination or JSON output. Ossie source remains preserved
for separately authorized authoring workflows. Built-ins are discoverable
without local declarations.

The result contains `project_id`, `generation_id`, `concepts`, `truncated` and
optional `next_cursor`. Concepts contain `concept_ref`, name, type, description
and extends. They do not contain mapping counts or SQL.

## Binding and evidence

Resolution accepts an exact `ontology_concept:<name>` ref, optional exact model
name, limit and cursor. The concept namespace is Project-local and
case-insensitive. Duplicate declarations and built-in shadows invalidate
resolution. Local inheritance must be acyclic and type-compatible; ValueTypes
reach one built-in value root and EntityTypes reach Any. Parent order is not
precedence. No external registry is consulted.

The manifest builds a separate immutable resolution index from the structured
Ossie `ontology_mappings[].concept_mappings[].object_mappings` shape. Only bare
logical `dataset.field` expressions can become canonical dimension targets.
Every applicable model is bound; multiple targets remain distinct candidates.
The field must be a dimension with a declared compatible datatype. Value roots
Boolean, Integer, Decimal, Float, String and Date require the identical Ossie
datatype; DateTime permits DateTime or DateTimeTz. No coercion is inferred.

An EntityType bare mapping additionally requires exactly one owned binary
OneToOne identifying relationship to a validated ValueType. Its datatype uses
the same compatibility rule. Compound identifiers and entity-valued roles are
not representable by a bare field. Valid compound expressions and referent/link
mappings remain preserved but are not promoted into query candidates.

Graph construction is bounded to 4,096 declarations, 4,096 mapping groups,
4,096 concept mappings, 128 inheritance depth and parents per concept, and
65,536 bound targets. Oversized or unprovable evidence invalidates resolution;
it never produces a silently truncated authorization decision.

## Visibility and ambiguity

Model, dataset and dimension visibility are checked before candidate
deduplication, order, state and pagination. Hidden targets are absent.
Visibility adapter failure, malformed decisions and panic return no partial
evidence. Resolution never runs [data policy](../operations/data-access-policy.md) as a substitute for
discovery visibility; ordinary compile/execute rechecks its own authorization
and data policy after the caller selects a candidate.

State describes the complete visible set: zero is `no_match`, one is `unique`,
more than one is `ambiguous`, including on a one-item page. Every candidate
contains canonical `ref`, model, dataset, dimension and deduplicated evidence.
Evidence names the mapped concept and `direct` or `descendant` match kind.
Subtype mappings supply supertype evidence, never the reverse. Direct evidence
sorts first but cannot suppress other targets or resolve ambiguity.

Cursors bind Project, concept/query, model bound, visibility scope and semantic
generation. A changed generation or visibility scope requires restarting
pagination; malformed or mismatched query cursors are INVALID_QUERY.

## Authoring and failures

Candidate loading and runtime manifest construction use the same binder.
Invalid evidence sets the resolution index unavailable while ordinary metadata
and compile-only behavior remain independent. The quality report makes invalid
graph, unbound field, unproven/mismatched datatype and unsupported bare entity
identifier findings blocking for publication. They cannot be downgraded by
advisory quality severity configuration. Valid unsupported forms and
non-queryable fields produce non-blocking warnings under the default threshold.
Diagnostics are source-attributed author evidence, not Agent response fields.

The closed diagnostic codes are `ONTOLOGY_GRAPH_INVALID`,
`ONTOLOGY_MAPPING_TARGET_INVALID`, `ONTOLOGY_MAPPING_TYPE_UNPROVEN`,
`ONTOLOGY_MAPPING_TYPE_MISMATCH`, `ONTOLOGY_ENTITY_IDENTIFIER_UNSUPPORTED`,
`ONTOLOGY_MAPPING_RESOLUTION_UNSUPPORTED`, and
`ONTOLOGY_MAPPING_TARGET_NOT_QUERYABLE`.

`ONTOLOGY_RESOLUTION_UNAVAILABLE` has caller action CHANGE_TARGET and HTTP 422.
It carries no raw mapping, hidden ref, Principal or adapter cause. Unknown
concepts and invalid requests use INVALID_QUERY; cancellation returns no partial
response. Fixed operation identities reuse bounded HTTP/MCP observation.

Tests live in `manifest`, `app/service/semantic`, `app/service/source` and
`tests/conformance/agentquery`; this metadata feature adds no SQL dialect path.
