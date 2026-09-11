# RFC-0085: Ontology-assisted Semantic Resolution Evidence

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-09
- **Last updated:** 2026-09-09
- **Scope:** Project-local Ossie ontology validation, mapping binding, and visibility-safe canonical semantic candidate evidence for Agents
- **Supersedes:** None

The current implementation contract is [ontology discovery and resolution](../../specs/semantic/ontology-resolution.md).

## Summary

Extend the existing discovery-only Ossie ontology index into a deterministic,
Project-local resolution-evidence service. Given one exact ontology concept,
Metis returns zero, one, or several visible canonical semantic dimension refs.
It never substitutes a candidate into a SemanticQuery, selects among ambiguous
candidates, generates an expression, or participates in SQL compilation.

The authority order is:

```text
Project authorization
  -> bounded metadata-only ontology concept discovery, when identity is unknown
  -> exact ontology concept lookup in one immutable Project generation
  -> ontology graph and mapping validation
  -> mapping expression parse and canonical binding
  -> asset visibility filtering
  -> no_match | unique | ambiguous candidate evidence
  -> Agent or caller explicitly selects a canonical semantic ref
  -> ordinary semantic query resolution
```

Ontology evidence narrows a caller's choices. The existing Resolver, Planner,
and compiler remain the only authorities for an explicitly submitted semantic
query.

## Motivation

Metis already loads and preserves Ossie `ontology` and `ontology_mappings`,
builds a concept index, and searches concept names, descriptions, `extends`,
and raw mapped-expression text. That is useful discovery, but it does not
answer a typed question such as "which visible queryable dimensions represent
Customer?" Raw strings are not canonical identities and cannot safely become
query input.

Agents need bounded evidence that connects business concepts to the governed
semantic surface without introducing a probabilistic or ontology-specific
compiler authority. The result must also preserve [Project authorization](../../specs/operations/project-action-authorization.md) and [asset visibility](../../specs/semantic/asset-governance.md): hidden mappings cannot influence
counts, result state, order, or diagnostics.

## Design

### Bounded concept discovery

RFC-0085 adds a dedicated, transport-neutral concept-discovery operation as a
required precursor to resolution. It is not sufficient to assume a production
Agent already knows or can guess `ontology_concept:<name>`:

```text
OntologyConceptSearchRequest {
  ProjectID
  Query              required bounded text
  Limit
  Cursor
}

OntologyConceptSearchResult {
  ProjectID
  GenerationID
  Concepts[] {
    ConceptRef
    Name
    Type
    Description
    Extends[]
  }
  Truncated
  NextCursor
}
```

The searchable document is derived only from safe concept metadata: canonical
name, description, declared type, and concept names in `extends`. Raw mapping
expressions, normalized mapping targets, dataset/field names reached only by a
mapping, and candidate counts are excluded from the index, matching, score,
match reasons, snippets, and cursors. A mapping therefore cannot cause a
concept to appear for a term that is absent from its safe metadata.

The operation requires `ProjectActionDiscover`, uses deterministic exact/name/
description matching and canonical tie-breaking, and follows RFC-0080 bounded
generation-scoped pagination. REST and MCP must expose equivalent request,
result, authorization, ordering, and error semantics. A production MCP Agent
can first call concept discovery, then pass its exact returned `ConceptRef` to
the resolution operation; neither step accepts a guessed mapping expression.

### Resolution is an explicit service, not query inference

The transport-neutral application contract is equivalent to:

```text
OntologyResolutionRequest {
  ProjectID
  ConceptRef
  Model              optional exact narrowing bound
  Limit              bounded presentation limit
  Cursor             optional generation-bound cursor
}

OntologyResolutionResult {
  ProjectID
  ConceptRef
  GenerationID
  State              no_match | unique | ambiguous
  Candidates[] {
    Ref               canonical dimension ref
    Model
    Dataset
    Dimension
    Evidence[] {
      MappedConceptRef
      MatchKind       direct | descendant
    }
  }
  Truncated
  NextCursor
}
```

Principal comes only from authenticated context. The operation requires
`ProjectActionDiscover` after normal Project resolution. REST and MCP expose
the same DTO and failure classes; transports do not parse ontology, apply
visibility, rank candidates, or infer query members.

`ConceptRef` is an exact `ontology_concept:<name>` identity returned by
the bounded concept-discovery service, not free-form natural language. Concept
discovery is the preceding step for a caller that does not know an identity.
`Model` can restrict the candidate set but cannot change mapping semantics or
rescue an invalid concept.

A `unique` result is still evidence. Metis never inserts its only candidate
into a query. The caller must submit the returned canonical ref through the
ordinary compile or execute contract, where all existing resolution,
authorization, visibility, and validation rules run again for that action.

### Project-local concept identity

Every resolution snapshot belongs to exactly one Project and one immutable
semantic generation. Concept identity is the Project plus a trimmed,
case-insensitive canonical name; authored spelling is retained only for
display. Case-fold collisions and duplicate local declarations across source
documents are invalid.

Concept origin is closed in V1:

- `local`: declared in an ontology document assembled into this Project;
- `builtin`: the Ossie built-in `Any`, `Boolean`, `Date`, `DateTime`,
  `Decimal`, `Float`, `Integer`, or `String`;
- `external_reference`: named by `extends`, a role, or a mapping but neither
  locally declared nor built in.

Built-ins are implicit reserved identities and cannot be locally redefined.
An external reference remains preservable discovery metadata but is not a
resolvable concept in V1. It produces a stable authoring diagnostic rather
than triggering network access, a global catalog lookup, or fallback to a
similarly named local concept. Importing external ontology packages and
versioned namespace registries require a later RFC.

There is no implicit local override. Multiple source documents form one
Project namespace; a declaration collision, built-in redefinition, or
case-only shadow is an error. This deliberately avoids environment-dependent
precedence based on file order.

No concept, mapping, cache entry, cursor, or candidate crosses Projects. Two
Projects may use the same concept name with unrelated definitions and advance
their semantic generations independently.

### Ontology graph validation

The immutable generation builds a typed concept graph before publishing its
resolution index. `extends` edges must reference a local or built-in concept,
preserve `EntityType` versus `ValueType` compatibility, and form an acyclic
graph. Every local `ValueType` must reach exactly the applicable built-in value
type contract; every local `EntityType` reaches `Any`, including its implicit
edge when omitted by the source specification.

Multiple inheritance is supported as a graph, not as a precedence list.
Parent order in YAML has no semantic effect. A direct mapping on a requested
concept is `direct` evidence. A mapping on any transitive subtype is
`descendant` evidence for that supertype because every mapped subtype instance
is also an instance of its supertypes. Evidence never flows from a supertype
down to a subtype.

The same canonical target reached through several parents is emitted once with
sorted, deduplicated provenance. Different targets remain different candidates;
direct evidence is not allowed to suppress inherited alternatives. Candidate
ordering may present direct evidence first and then use canonical-ref order,
but it never changes `ambiguous` into `unique`.

Cycles, unknown parents, invalid cross-type inheritance, conflicting concept
types, and unsupported external dependencies prevent the resolution index from
becoming ready. Discovery may retain authoring diagnostics, but runtime
resolution fails closed instead of using a partial graph.

### Mapping parse, binding, and normalized targets

The current index's `MappedExpressions []string` is legacy search text, not
resolution evidence. It must leave all Agent-facing discovery indexes, match
documents, scoring, reasons, snippets, and responses when this RFC is
implemented. The implementation must instead parse the structured Ossie
mapping shape and associate each expression with its actual object, referent,
relationship, and role target. It cannot continue recursively inheriting the
nearest `concept` string without understanding that structure.

V1 promotes only an `object_mappings.expression` that parses and binds as one
bare logical `dataset.field` reference. Binding occurs against every applicable
semantic model in the same immutable Project generation. A successful target
must be a queryable dimension and normalizes to the existing canonical form:

```text
dimension:<model>.<dataset>.<field>
```

Bare-field binding is necessary but not sufficient. The mapped concept and
field must also satisfy a closed V1 semantic proof.

For a `ValueType`, its validated `extends` closure must reach exactly one Ossie
built-in value root. The dimension's declared Ossie datatype must match this
matrix without an inferred cast:

| Built-in value root | Compatible dimension datatype |
| --- | --- |
| `Boolean` | `Boolean` |
| `Integer` | `Integer` |
| `Decimal` | `Decimal` |
| `Float` | `Float` |
| `String` | `String` |
| `Date` | `Date` |
| `DateTime` | `DateTime`, `DateTimeTz` |

Missing datatype, `Time`, `Opaque`, multiple built-in roots, or any other pair
is not proven compatible. V1 does not infer numeric widening, string parsing,
timezone conversion, or a dialect cast from the mapping expression.

For an `EntityType`, one bare field is candidate evidence only for a proven
simple identifier. The entity must declare exactly one `identify_by`
relationship; that relationship must belong to the entity, be binary, be
`OneToOne`, and have one additional role played by a validated `ValueType`.
The mapped dimension datatype must be compatible with that identifier
ValueType under the same matrix. An absent or compound identifier, a
multi-role relationship, an entity-valued role, unknown relationship, or
weaker multiplicity cannot be represented by one bare field in V1. It requires
the appropriate Ossie `referent_mappings` shape and remains outside resolution
evidence until that shape is supported. The built-in entity root `Any` cannot
itself be proven by a bare field.

Mapping diagnostics distinguish invalid claims from supported Ossie shapes
that V1 merely cannot expose as query candidates:

| Condition | Stable diagnostic | Validation effect |
| --- | --- | --- |
| parse failure or an unbound bare-field form | `ONTOLOGY_MAPPING_TARGET_INVALID` | error; resolution index not ready and candidate not publishable |
| ValueType root/datatype cannot be proven | `ONTOLOGY_MAPPING_TYPE_UNPROVEN` | error; resolution index not ready and candidate not publishable |
| declared datatype conflicts with proven value root | `ONTOLOGY_MAPPING_TYPE_MISMATCH` | error; resolution index not ready and candidate not publishable |
| bare field used for an unproven or compound EntityType identifier | `ONTOLOGY_ENTITY_IDENTIFIER_UNSUPPORTED` | error; resolution index not ready and candidate not publishable |
| valid referent/link/role or compound expression not supported by V1 resolution | `ONTOLOGY_MAPPING_RESOLUTION_UNSUPPORTED` | bounded non-blocking authoring diagnostic; mapping preserved but excluded |
| valid mapped field is not a queryable dimension | `ONTOLOGY_MAPPING_TARGET_NOT_QUERYABLE` | bounded non-blocking authoring diagnostic; mapping preserved but excluded |

Diagnostics carry source location, concept identity, and a bounded reason
variant. Agent responses never receive the raw expression or a hidden target.
Source validation uses RFC-0076 quality output and cannot silently downgrade
an invalid mapping into the non-blocking unsupported class.

Unqualified fields, constants, functions, casts, arithmetic, predicates,
multiple-field expressions, raw physical columns, and expressions with
dialect-dependent meaning are not candidate evidence. `referent_mappings`,
`link_mappings`, ontology derivations, relationships, and role traversal are
preserved but do not resolve query members in V1. They receive stable,
source-attributed authoring diagnostics rather than being guessed into a
dimension, metric, join, or SQL expression.

If a dataset name binds in more than one semantic model, each normalized
dimension remains a candidate unless an explicit request `Model` bound removes
it. A non-dimension field cannot become an Agent-queryable ref merely because
it is mapped. Ontology mappings never infer metrics in V1; a metric must remain
an explicitly authored, discovered, and selected semantic metric.

Mapping validation runs in the shared source-loading path used by offline
validation and runtime generation construction. Invalid resolution evidence
must be visible to authors before use. Runtime never reparses a different
mapping interpretation on demand.

### Visibility, leakage, and deterministic result state

Project authorization runs before concept lookup. For every bound target,
[Asset visibility contract](../../specs/semantic/asset-governance.md) model, dataset, and dimension visibility is evaluated before
deduplication, candidate counts, state construction, sorting, truncation, or
cursor creation. A hidden target behaves as absent and cannot be detected
through `State`, counts, order, latency classes, cursor shape, or error details.

The state is computed over the complete visible candidate set:

- zero visible canonical refs returns `no_match`;
- exactly one returns `unique` and that one candidate;
- more than one returns `ambiguous`, even if a presentation limit returns one
  page or one candidate has stronger display evidence.

Ranking can order evidence for navigation but can never resolve ambiguity.
Stable order is `MatchKind` followed by canonical ref. Cursors bind Project,
exact concept ref, optional model bound, visibility scope, and generation using
the existing RFC-0080 pagination rules. They do not contain raw mappings.

The existing discovery response's raw `mapped_expressions` must not remain a
general `discover` disclosure path after this RFC is implemented: expression
text can name a hidden asset. Removing that response field alone is
insufficient: raw mapping text and mapping-derived target names must also be
absent from concept search documents, index postings, query matching, ranking,
match reasons, and pagination. Agent discovery returns concept metadata only;
visible canonical mappings come from this resolution service. An authoring or
administration surface may inspect raw mapping source only after its existing
`author` or `admin` authorization and must not reuse the Agent DTO or index.

Resolution logs, traces, metrics, public errors, and cursor payloads do not
contain raw mapping expressions, hidden refs, Principal claims, descriptions,
or AI context. Low-cardinality operation outcome and bounded candidate-count
buckets are sufficient observation.

### Generation, cache, and failure behavior

The validated concept graph and bound mapping index are immutable derived read
models owned by one Project runtime generation. They publish atomically with
that generation and can be rebuilt from its SemanticManifest. There is no
global ontology generation, mutable cross-Project cache, or independently
advancing resolver snapshot.

An activation changes only the affected Project's next requests. In-flight
requests retain one generation from concept lookup through result construction.
Invalid cursor generation returns the existing restart-pagination action.
Cancellation, index unavailability, invalid mapping state, and visibility
adapter failure return no partial candidates and never fall through to search
ranking or raw expression matching.

### Reference boundary

Apache Ossie defines concepts, built-ins, `extends`, and ontology mappings from
logical-model field expressions. Metis follows that vocabulary and preserves
unsupported shapes; it adds only the Project-local validation, canonical
binding, visibility, and Agent response contract required to serve them
safely. See the [Apache Ossie ontology
specification](https://github.com/apache/ossie/blob/main/ontology/ontology.md).

Cube's mature semantic product treats curated Views as the consumer and Agent
surface, while descriptions and AI context improve query generation without
replacing explicit measures and dimensions. Metis adopts that separation:
ontology enriches the Agent's selection context, while canonical semantic refs
remain the query contract. Metis does not copy Cube's model vocabulary or use
free-form AI context as resolution authority. See Cube's [Views](https://docs.cube.dev/docs/data-modeling/views)
and [AI context](https://docs.cube.dev/docs/data-modeling/ai-context).

## Alternatives

Automatically choosing the highest-ranked mapped member is rejected because
rank is presentation evidence, not semantic authority. Passing ontology refs
directly to the compiler is rejected because it creates a second resolver and
makes ambiguity caller-dependent. Returning raw mapping SQL is rejected
because it leaks implementation details and bypasses canonical query contracts.
A global ontology service is deferred because Project-local definitions,
visibility, and independent generations are already the product boundary.

## Rollout and migration

1. Remove raw mapping text and mapping-derived targets from Agent discovery
   response, indexing, matching, scoring, reasons, and cursor inputs; add the
   bounded REST/MCP concept-discovery contract.
2. Replace the string walker with a typed, source-attributed ontology graph and
   mapping validator, including ValueType compatibility and simple EntityType
   identifier proof.
3. Bind supported object-mapping expressions to immutable canonical dimension
   refs and publish the index with each Project generation.
4. Add the transport-neutral resolution service and stable no/one/many result
   vocabulary.
5. Expose equivalent bounded REST and MCP operations.
6. Update authoring diagnostics, public contracts, and conformance evidence.

Until all steps land, ontology-assisted resolution remains unsupported by
design and existing ontology search must not be described as resolution.

## Test and acceptance criteria

- Exact local, built-in, and unresolved external concept identities have
  stable results; duplicate, case-colliding, and built-in-shadowing declarations
  fail deterministically.
- Cycle, unknown-parent, cross-type, multiple-inheritance, and diamond graphs
  produce the specified validation and deduplication behavior independent of
  document and list order.
- Bare logical field mappings bind to canonical dimensions; compound,
  ambiguous, non-dimension, referent, link, role, and unsupported expressions
  never become candidates and carry stable source diagnostics.
- ValueType mappings satisfy the exact datatype matrix; missing or conflicting
  types fail with stable diagnostics and no inferred coercion.
- EntityType bare mappings require a binary `OneToOne` simple identifier whose
  ValueType matches the dimension; absent, compound, entity-valued, or otherwise
  unproven identifiers never become candidates.
- Direct and descendant evidence flows only in the specified direction and
  never suppresses a distinct candidate.
- Asset visibility runs before counts, state, ordering, truncation, and cursor
  construction; hidden-only and truly unmapped concepts are externally
  indistinguishable.
- Zero, one, and many visible candidates return `no_match`, `unique`, and
  `ambiguous`; ranking and pagination never convert many into one.
- Returned refs compile only after the caller explicitly selects and submits
  them through the ordinary semantic query API.
- REST and MCP results are structurally equivalent and never return raw
  mappings or SQL.
- REST and MCP both support the bounded concept-discovery-to-resolution flow
  when the caller initially knows no ConceptRef.
- The production MCP tool inventory exposes both concept discovery and
  resolution and does not depend on a generic semantic-search tool being
  present.
- Searching any hidden dataset or field name that appears only in raw mapping
  text cannot change concept matches, scores, reasons, truncation, cursors, or
  timing class; removing the response field alone does not satisfy this test.
- Project A activation, ontology names, mappings, and cursors cannot affect
  Project B, including when both Projects use identical concept names.

## Non-goals

- accepting an ontology concept in `SemanticQuery`;
- automatic metric, dimension, join-path, or SQL selection;
- ontology relationship execution, reasoning, derivation, or constraint
  evaluation;
- external ontology registry fetch, cross-Project ontology composition, or
  namespace/package version negotiation;
- embeddings, LLM ranking authority, synonym generation, or fuzzy matching;
- changing [Project authorization](../../specs/operations/project-action-authorization.md) or [asset visibility](../../specs/semantic/asset-governance.md) semantics.

## Documentation updates

Implementation must update semantic discovery and MCP design, public Agent
contracts, authoring/quality diagnostics, asset visibility, Project generation
ownership, and ontology conformance documentation.
