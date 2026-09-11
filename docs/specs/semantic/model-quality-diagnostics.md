# Deterministic Model-quality Diagnostics

This specification defines the project-scoped quality report produced after a
semantic candidate has passed Ossie loading, multi-document assembly, and
`SemanticManifest` construction. Quality evaluation is part of the shared
`app/service/source` candidate loader used by runtime bootstrap and the
offline authoring CLI.

## Report contract

A quality report contains its schema version, Project ID, candidate content
digest, effective publication threshold, publishability, and ordered
diagnostics. Each diagnostic contains a stable code, severity, primary
canonical asset reference, related canonical references where applicable,
source location when known, message, `CHANGE_MODEL` caller action, and bounded
structured evidence.

For identical project bytes and policy, the JSON result MUST be deterministic.
Diagnostics sort by severity (`error`, `warning`, then `info`), code, primary
canonical reference, and location, with a deterministic full-record tie-break.
Related references and unordered evidence collections MUST also be sorted.

The JSON shape is versioned independently from the immutable release bundle:

```json
{
  "schema_version": 1,
  "project_id": "finance",
  "content_digest": "sha256:...",
  "publication_threshold": "warning",
  "publishable": false,
  "diagnostics": [
    {
      "code": "MODEL_QUALITY_ALIAS_CONFLICT",
      "severity": "warning",
      "asset": "metric:sales.revenue",
      "related_assets": ["metric:growth.revenue"],
      "location": {"path": "models/sales.ossie.yaml", "line": 7, "column": 3},
      "message": "governed metric alias is claimed by multiple canonical metrics",
      "caller_action": "CHANGE_MODEL",
      "evidence": {"reason": "governed_alias", "normalized_identity": "gmv"}
    }
  ]
}
```

`code`, `severity`, and `caller_action` use named, closed domain types in the
shared Go contract. A location is an object rather than a formatted string:
`path` is required when the object exists, while one-based `line` and `column`
are optional until the source loader can provide them. Consumers MUST NOT
parse `message` or `location.path` to recover these fields.

`evidence.reason` distinguishes multiple evidence variants under one stable
diagnostic code. Other evidence fields are code-and-reason-specific and remain
machine values rather than formatted prose.

`valid` and `publishable` are distinct. A candidate that passes structural and
semantic validation remains valid when it has advisory findings. It is not
publishable when at least one finding meets its configured publication
threshold. `s2s validate-project` always emits the structured result and exits
non-zero when either value is false.

## Registered diagnostics

Ontology structural and mapping findings are also projected into the report.
Unlike advisory model-quality rules, invalid ontology evidence is unconditionally
publication-blocking and cannot be downgraded. Valid unsupported mappings remain
advisory. See [ontology resolution](ontology-resolution.md) for the boundary.

The current registry contains:

| Code | Evidence reason | Meaning |
| --- | --- | --- |
| `MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION` | `exact_definition` | Different canonical metrics in one model have exactly equal authored datatype, expression, and extensions. |
| `MODEL_QUALITY_ALIAS_CONFLICT` | `governed_alias` | One normalized governed alias is claimed by multiple canonical metrics. |
| `MODEL_QUALITY_DISCOVERY_IDENTITY_COLLISION` | `canonical_or_alias_identity` | Canonical metric names and governed aliases normalize to an ambiguous discovery identity. |
| `MODEL_QUALITY_DISCONNECTED_DATASET` | `no_metric_source_path` | A dataset cannot be reached from any resolved metric source dataset. |
| `MODEL_QUALITY_UNREACHABLE_DIMENSION` | `disconnected_dataset` | A dimension belongs to such a disconnected dataset. |
| `MODEL_QUALITY_AMBIGUOUS_RELATIONSHIP_PATH` | `multiple_shortest_paths` | Multiple shortest relationship paths connect a metric source dataset to a target dataset. |
| `MODEL_QUALITY_METRIC_SOURCE_UNRESOLVED` | `metric_source_resolution` | A metric source dataset cannot be resolved deterministically. |
| `MODEL_QUALITY_METRIC_TYPE_MISMATCH` | `declared_expression_type` or `structured_expected_type` | Authored datatype, analyzed expression type, or a typed expected-type claim contradicts another item. |
| `MODEL_QUALITY_METRIC_AGGREGATION_MISMATCH` | `row_dependent_scalar` or `structured_expected_aggregation` | Analyzed aggregation evidence contradicts a typed claim, or a scalar expression remains row-dependent. |
| `MODEL_QUALITY_METRIC_EXPRESSION_EVIDENCE_INCONSISTENT` | `dialect_type_or_aggregation` | Dialect expressions expose inconsistent concrete type or aggregation evidence. |
| `MODEL_QUALITY_DEPRECATED_ASSET_WITHOUT_REPLACEMENT` | `deprecated_without_replacement` | Authored deprecated governance evidence has no canonical replacement. |
| `MODEL_QUALITY_CERTIFIED_ASSET_WITHOUT_OWNER` | `certified_without_owner` | Authored certified governance evidence has no owner. |

Every registered rule is a warning by default. Exact definition equivalence is
evidence only: Metis MUST NOT merge, rename, rank, select, or rewrite either
metric. Structural errors still fail candidate construction before quality
evaluation.

Lifecycle, certification, ownership, and replacement diagnostics consume only
the typed `asset_governance` extension specified by
[`asset-governance.md`](asset-governance.md). Quality evaluation MUST NOT infer
them from descriptions, labels, or AI context.

## Evidence authority

Rules MAY consume authored Ossie fields, parsed and bound expressions,
manifest dependency and relationship graphs, governed Agent aliases, and typed
Metis extensions. They MUST NOT query warehouse data, call an LLM, or tokenize,
pattern-match, or interpret free-form `description` or `ai_context` content.

The METIS metric extension `quality_claims` supplies optional diagnostic-only
evidence:

```yaml
custom_extensions:
  - vendor_name: METIS
    data: '{"kind":"quality_claims","expected_type":"Decimal","expected_aggregation":"aggregate"}'
```

At least one claim is required. `expected_type` MUST be an Ossie datatype and
`expected_aggregation` MUST be `scalar`, `aggregate`, or `mixed`. These claims
do not change expression evaluation, resolution, planning, or compilation.
Malformed claims fail Ossie validation rather than being ignored.

Unknown non-METIS extension data remains opaque and preserved. When exact
extension-bearing definitions produce a finding, source attribution points to
the document containing each affected asset.

## Project policy and publication

The optional semantic project policy is:

```yaml
semantic_sources:
  models:
    path: ./models/*.ossie.yaml

quality:
  severities:
    MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION: error
  publication_threshold: error
```

Severity overrides MUST name a registered diagnostic code and use `error`,
`warning`, or `info`. The publication threshold uses the same closed values
and defaults to `error`. A finding marks the report as not publishable when its
effective severity is at least as severe as the threshold. Thus default warning findings
are advisory; setting the threshold to `warning`, or promoting a rule to
`error`, makes the applicable finding blocking.

Policy changes alter effective severity and publishability only. They do not
alter evidence, the candidate content digest, Ossie interpretation, or runtime
loading. Hosts can use the report when deciding whether to accept a source
change. This package does not create persistent publication state.

## Surfaces and conformance

`source.ValidateProject` and `s2s validate-project` return byte-equivalent JSON
for the same input. `s2s inspect-project` includes the complete quality report.
No quality-scanning MCP tool exists. Primary Agent discovery MAY expose a
bounded already-computed warning for a selected asset in a future additive
projection, but MUST NOT independently reimplement the rules.

Conformance covers stable output, equivalent-definition warnings, governed
alias and canonical-name collisions, multi-document source attribution,
typed-claim and graph findings, prose non-interpretation, policy-controlled
quality eligibility, and CLI/shared-service parity.
