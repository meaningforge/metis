# RFC-0089: Catalog-assisted Ossie Authoring

- **Status:** Draft
- **Owners:** TBD during review
- **Created:** 2026-09-25
- **Last updated:** 2026-09-25
- **Scope:** Core authoring CLI, catalog metadata, deterministic Ossie generation
- **Supersedes:** None
- **Numbering:** Proposed, subject to repository review.

## Summary

Add a small authoring workflow that describes explicitly selected relations in a
configured DataSource, captures a portable metadata snapshot, and generates a
reviewable Ossie project skeleton. A source-controlled authoring map identifies
which fields should become dimensions and any explicitly requested starter metrics.

Database facts and authored business meaning have different provenance. Generated
files remain local review candidates until the author validates and activates them.
The initial implementation supports Doris and ClickHouse; optional DuckDB follows
the same catalog contract without changing the default CGO-free binary.

All new commands and file formats below are proposals, not current interfaces.

## Motivation

Metis currently provides validation, formatting, inspection, and project comparison.
Users still manually transcribe physical relations and columns into Ossie, then
learn the difference between fields, dimensions, metrics, and relationships.
Repeated physical transcription is a useful automation target; business semantics
such as revenue, uniqueness, and additivity require explicit author decisions.

## Design

### Authoring workflow

```sh
# New online authoring command: reads metadata only.
metis inspect-source --config ./metis.yaml --project sales \
  --data-source warehouse --relations ./authoring/relations.json \
  --output ./authoring/catalog.json

# New offline command: reproducible from checked-in inputs.
s2s init-project --catalog ./authoring/catalog.json \
  --mapping ./authoring/model-map.yaml --output ./candidate-sales

# Existing validation command; optional online validation is RFC-0088.
s2s validate-project --project sales --config ./candidate-sales/project.yaml
```

`inspect-source` can run before a semantic model exists. It loads deployment
configuration and the selected Project registration but does not bootstrap its
semantic sources. It verifies that the selected DataSource is applied to that
Project, authorizes Project `author`, and uses bounded Runner catalog inspection.
This is a deliberate authoring-only assembly path. Runtime serve/bootstrap rules
remain unchanged and still require valid semantic sources.

Local execution is trusted authoring under the operator's OS/database identity.
There is no REST/MCP route or Agent-controlled catalog tool in this version.
Embedders must authorize authors before physical metadata is returned; query
visibility permissions alone do not authorize catalog inventory.

The relation selector file uses an explicit list of structured names:

```json
{
  "schema_version": 1,
  "relations": [
    {"id": "orders", "parts": ["sales", "public", "orders"]}
  ]
}
```

Identifier-part meaning follows the selected backend; the example is illustrative
and does not prescribe three-part names for every engine. Wildcards, full-database
crawling, user SQL, views' SQL definitions, and row sampling are excluded.

### Shared catalog contract

Reuse RFC-0088's exact-object `CatalogInspector` capability and Runner lifecycle.
Only that small primitive is a prerequisite; the complete online-validation CLI
is not a dependency of the offline generator. Do not introduce another connection
profile, pool, secret resolver, SQL executor, or physical placement authority.

Catalog schema version 1 contains backend family, object and column identifiers,
native types with precision/scale/timezone evidence, nullability as known/unknown,
and key metadata with its actual backend meaning. Stable normalized content has
a digest; observation time and server-version observations are separate metadata.
The snapshot is evidence collected from a database, not semantic authority and
not a permission token. It must not contain credentials, endpoints, row values,
source view SQL, or automatic samples. Column comments are excluded initially.

Limits match the shared inspection facility: at most 200 selected relations,
10,000 columns, 10 MiB metadata, a 5-minute total deadline and a 30-second
per-operation deadline, tightened by deployment ceilings. Missing permissions,
unreadable relations, and overflow produce a failure report rather than a silently
partial successful catalog. No background synchronization is introduced.

### Explicit authoring map

```yaml
schema_version: 1
project: sales
model: sales
data_source: warehouse
datasets:
  - relation: orders
    name: orders
    fields:
      - column: region
        name: region
        dimension: true
      - column: amount
        name: amount
starter_metrics:
  - name: order_rows
    kind: row_count
    dataset: orders
```

The map is strict and closed. It explicitly selects physical columns; unselected
columns are not emitted. `starter_metrics` is optional. V1 supports only an
explicit `row_count` of a selected relation, labeled as a technical row count;
it does not assert distinct orders or another business entity count.
No numeric column is automatically summed, averaged, or converted to a metric.

The first version does not generate join relationships. Observed foreign-key or
key metadata may be included in a separate author review report, with provenance.
Doris key-model declarations and ClickHouse primary/sorting keys must not be
translated into unique semantic keys or many-to-one cardinality guarantees.
An author adds and validates semantic relationships after reviewing the facts.

### Deterministic generation

For identical snapshot bytes, authoring map, and generator version, output bytes
are identical. Use Metis's supported Ossie version and canonical source formatter.
Store generation provenance and pending review items in a sidecar, not invented
semantic fields or compiler-interpreted prose. Preserve physical case and quoting.

Logical names must follow the current loader's grammar. Authors provide explicit
names; invalid/reserved names and collisions are reported with suggested repairs,
never resolved by silently renaming. Relative output paths cannot escape the output
directory or follow symlinks into another project.

Type mappings are backend-specific and tested. Preserve Decimal versus Float and
Date versus timestamp distinctions. A native type that cannot be mapped faithfully
requires explicit column exclusion or an authored supported mapping. Unknown
types are not silently coerced to String, and lossy casts are not generated.
Generate dialect-specific direct-column expressions when ANSI syntax cannot
represent the physical identifier unambiguously.

The optional `data_source` authoring-map field emits the existing model-level
METIS placement extension. It contains only the logical source name; connection
configuration and credentials remain outside Ossie. Generation operates on one
source snapshot per invocation and produces no cross-source relationship.

### Generated project

```text
candidate-sales/
  project.yaml
  models/sales.ossie.yaml
  authoring-report.json
  GETTING_STARTED.md
```

`project.yaml` uses the existing semantic_sources grammar. The model contains
selected datasets and fields, explicitly selected dimensions, and optional
starter metrics. A metric-free skeleton is permitted by the current Ossie loader;
the authoring report clearly records that business metrics have not been authored.

`authoring-report.json` records source digest, mapping digest, generator version,
physical-to-canonical mappings, warnings, and explicit review tasks. It separates
generated facts from pending decisions about business definitions, time zones,
calendar conventions, relationship cardinality, and aggregation behavior.
It embeds the existing offline validation/quality result without changing its
meaning: `publishable` is only a quality threshold, not business certification.

Run the shared loader, formatter, and offline quality evaluator on staged output.
Structural failure prevents publishing the candidate directory. Advisory quality
warnings remain visible. Successful generation means a structurally valid candidate
was produced, even when authoring is incomplete; it never means ready to deploy.
`GETTING_STARTED.md` lists the exact next commands and registration requirements.

### File and error behavior

Refuse an existing output directory; V1 has no force/merge/in-place update mode.
Stage all files privately on the same filesystem, validate them, then atomically
rename the directory. A failure leaves no advertised partial project. Use
owner-only file permissions. Re-running into a different directory and using
the existing `s2s diff-project` is the supported review workflow.

Unknown schema versions/fields, invalid mappings, identifier collisions, missing
selected relations/columns, and unsupported types have stable authoring diagnostic
codes with file/field locations. Mapping/model repairs use `CHANGE_MODEL`;
inspection capability or credentials use `CHANGE_TARGET`. Native errors and
secret values are redacted centrally. Do not derive actions by parsing messages.

Generation exits 0 on a validated candidate with a complete report, 1 on a
generation/inspection finding that prevents output, and 2 on command/input/I/O
failure. Pending author review is visible in the report and does not silently
activate the candidate. Normal `validate-project` eligibility semantics still
apply before an author adopts it.

## Alternatives

- Infer business metrics and relationships with an LLM: useful as a separate
  author aid later, but it cannot be the authority for deterministic generation.
- Convert every numeric column to SUM: produces incorrect metrics for identifiers,
  balances, prices, ratios, and snapshots.
- Expose all columns as dimensions: bypasses deliberate semantic publication and
  may disclose technical or sensitive fields. Require an explicit selection.
- Import every dbt/Cube format in the first release: expands the compatibility
  problem before the native authoring workflow is proven. Future importers should
  feed the same reviewed Ossie candidate boundary with explicit loss reports.

## Rollout and migration

1. Implement offline `init-project` from versioned catalog fixtures and explicit
   maps; it can ship before online inspection.
2. Reuse the shared catalog capability for Doris/ClickHouse `inspect-source`.
3. Add complete Doris and ClickHouse tutorials using small disposable datasets,
   environment-based credentials, a first business metric, compilation, and query.
4. Verify optional DuckDB support in its existing build flavor.

Existing authored documents are never regenerated or overwritten. Adoption is
manual through normal source control and existing runtime registration/activation.
The CLI creates no database objects, Git repository, Cloud release, or deployment.

## Test and acceptance criteria

- Reordered catalog responses produce the same normalized snapshot and output.
- Reserved names, case-sensitive identifiers, Decimal precision, temporal types,
  nullable fields, unsupported native types, and duplicate names are discriminating
  test fixtures; generated output passes the current loader and formatter.
- Generation never invents metric business meaning or semantic uniqueness from
  ClickHouse sorting/primary keys or Doris key-model metadata.
- A selected subset cannot leak unselected column definitions into model output.
- Fresh generation works before a Project has valid semantic sources; serving
  that same incomplete Project still fails existing runtime bootstrap validation.
- Online inspection shares production admission, secrets, cancellation, and cleanup;
  no source row SELECT is performed by the catalog path.
- Failure preserves any existing directory and does not expose partial output.
- An unfamiliar developer can follow each checked-in backend tutorial from
  physical tables to one validated and executed semantic query. A suggested
  usability target is 30 minutes after the database is reachable, measured and
  reported as a target rather than assumed achieved.

## Documentation updates

Update the source-authoring specification, CLI reference, Renderer/Driver extension
guidance, example index, and add dedicated Doris/ClickHouse onboarding guides at
implementation time. Link this Draft in the RFC index during proposal review.

## References and review decisions

- [Source authoring contract](../../specs/semantic/asset-authoring-lifecycle.md)
- [Model quality diagnostics](../../specs/semantic/model-quality-diagnostics.md)
- [Runtime placement](../../specs/operations/runtime-bootstrap.md)
- [Current Ossie validation](../../../ossie/validate.go)
- [Current sales example](../../../examples/demo/models/sales.ossie.yaml)

Confirm type mapping and physical identifier escaping for both initial engines.
Decide whether explicitly requested row_count belongs in the first release or
only in the tutorial; the safe default remains no generated metrics. The authoring
map is tooling input and must not grow into a second runtime semantic schema.
