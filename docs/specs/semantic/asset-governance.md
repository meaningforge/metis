# Semantic Asset Governance and Visibility

This specification defines typed governance evidence and the transport-neutral
asset-visibility boundary. Project authorization is
always evaluated first; asset visibility cannot grant a denied project action.

Visibility is not row/column data authorization. Configured
[data access policy](../operations/data-access-policy.md) runs after visible
query assets are authorized and canonical workload dependencies are resolved.
Policy-only filter fields need not be caller-visible, but cannot become
discovery, explanation or output-schema evidence.

## Authored metadata

Apache Ossie remains the semantic source of truth. Because Ossie 0.2.0.dev0 has
no native ownership, lifecycle, certification, replacement, or visibility-tag
fields, Metis carries one typed custom extension on semantic models, datasets,
dimension fields, metrics, and relationships:

```yaml
custom_extensions:
  - vendor_name: METIS
    data: '{"kind":"asset_governance","owner":"team:finance","lifecycle":"deprecated","certification":"certified","deprecation_date":"2026-12-31","replacement":"metric:sales.net_revenue","policy_tags":["finance","internal"]}'
```

The closed lifecycle values are `active` and `deprecated`; the closed
certification values are `uncertified` and `certified`. Missing values default
to `active` and `uncertified`. `deprecation_date` uses `YYYY-MM-DD`.
`deprecation_date` and `replacement` are valid only for deprecated assets.

Replacement refs are canonical, must exist in the assembled Project, must name
the same asset kind, must not be self-references, and must be acyclic:

```text
model:<model>
dataset:<model>.<dataset>
metric:<model>.<metric>
dimension:<model>.<dataset>.<field>
relationship:<model>.<relationship>
```

At most 32 distinct policy tags are accepted. Tags are lowercase identifiers
of at most 64 characters using letters, digits, `.`, `_`, `:`, or `-`; output
and policy input sort them deterministically. Unknown fields in this METIS
extension fail validation. Unknown extension kinds and non-METIS vendors remain
opaque and survive round trips.

## Visibility policy API

`app/service/semantic` owns the policy contract:

```text
AssetVisibilityRequest {
  Principal  *auth.Principal
  ProjectID  string
  Action     ProjectAction
  AssetRef   string
  AssetKind  model | dataset | metric | dimension | relationship
  PolicyTags []string
}

AssetVisibilityDecision {
  Effect visible | hidden
  Reason bounded_reason
}

AssetVisibilityPolicy.EvaluateAssetVisibility(context, request) -> decision
```

Only authenticated identity, resolved Project ID, closed action, canonical
asset identity, kind, and validated tags cross this boundary. Descriptions,
AI context, query values, SQL, credentials, tokens, and policy-engine messages
do not. Missing policies, invalid effects, unregistered reasons, malformed
requests, and malformed governance evidence fail closed.

`AllVisibleAssetPolicy` is the explicit compatibility adapter installed by the
discovery constructor. It does not bypass project authorization. Deployments
replace it with a membership or catalog policy without changing REST, MCP,
compile, or execution DTOs. Explicitly passing a nil policy to bootstrap,
including a typed nil, rejects initialization; omitting the option retains the
standalone default.

A policy whose decisions can change independently of a semantic generation
implements the optional `AssetVisibilityScopeProvider`. Its bounded scope value
must change whenever visible membership changes. Discovery cursors hash that
value together with policy type, Principal identity and scopes, resolved
Project, query shape, and Project generation. Policies without the provider are
scoped by policy type and Principal and therefore must not mutate decisions
during the lifetime of a generation.

## Enforcement and non-disclosure

Every operation follows this order:

```text
authenticate
  -> resolve Project
  -> authorize Project x action
  -> resolve canonical asset identity and typed governance
  -> evaluate asset visibility
  -> construct discovery result or continue compile/execution
```

Model visibility is inherited by its children. Dataset visibility is inherited
by its dimensions and by metrics whose resolved sources use that dataset.
Relationships also require both endpoint datasets to be visible.

Filtering happens before ranking, limits, pagination, cursors, ambiguity lists,
equivalence hints, compatibility results, and relationship traversal. Direct
detail, compile, validation, explanation, and execution references are checked
again. A hidden asset returns its existing kind-specific not-found error rather
than a distinguishable authorization error. Execution rejects hidden query
assets before resolving a DataSource, Backend, secret, or Driver.

REST and MCP are projections of these application services and therefore expose
the same visible semantic universe. Ontology concepts are outside this asset-visibility contract and
are not passed to `AssetVisibilityPolicy`.
Their [resolved dimension candidates](ontology-resolution.md) do pass through
model, dataset and dimension visibility before ambiguity and pagination.
Raw mappings are absent from all Agent search indexes and responses; concept
metadata is not a back door to hidden semantic identities.

## Evidence and warnings

Agent summaries and semantic-search items expose a `governance` object with
`owner`, effective `lifecycle`, effective `certification`, optional
`deprecation_date`, and optional canonical `replacement`. Policy tags are not
returned as selection evidence.

Deprecated assets remain usable. Compiled and primary execution results include
a deterministic `warnings` array when selected assets are deprecated:

```json
{
  "code": "SEMANTIC_ASSET_DEPRECATED",
  "asset_ref": "metric:sales.old_revenue",
  "deprecation_date": "2026-12-31",
  "replacement": "metric:sales.net_revenue"
}
```

Warnings never rewrite a query. Certification is evidence only and never
changes ranking or chooses a metric.

## Source comparison and quality integration

Governance extensions are part of canonical asset digests, so ownership,
lifecycle, certification, replacement, and tag changes appear in semantic
source comparisons as modified assets. The quality registry emits advisory warnings
by default for:

- `MODEL_QUALITY_DEPRECATED_ASSET_WITHOUT_REPLACEMENT`;
- `MODEL_QUALITY_CERTIFIED_ASSET_WITHOUT_OWNER`.

Their evidence uses typed governance fields only. Descriptions and AI context
are never interpreted as governance.

## Reference boundary

The design follows Cube's mature separation between metadata exposure and
query-time enforcement, and its use of authenticated security context as policy
input. Metis does not copy Cube's combined member/row/masking policy model:
row filters, masking, data entitlements, approval storage, glossaries, and a
general policy language remain outside this contract.

- [Cube access policies](https://docs.cube.dev/docs/data-modeling/data-access-policies)
- [Cube member-level security](https://docs.cube.dev/docs/data-modeling/access-control/member-level-security)
- [Cube views](https://docs.cube.dev/docs/data-modeling/views)
