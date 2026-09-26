# RFC-0089: Catalog-assisted Ossie Authoring

- **Status:** Draft
- **Owners:** meaningforger
- **Created:** 2026-09-25
- **Last updated:** 2026-09-26
- **Scope:** Core authoring CLI, catalog metadata, deterministic Ossie generation
- **Supersedes:** None

## Summary

Add a small authoring workflow that describes explicitly selected relations in a
configured DataSource, captures a portable metadata snapshot, and generates a
reviewable Ossie project skeleton. A source-controlled authoring map identifies
which fields should become dimensions and any explicitly requested starter metrics.

Database facts and authored business meaning have different provenance. Generated
files remain local review candidates until the author validates and explicitly
adopts them; generation never publishes or activates a semantic model.
The initial implementation supports Doris and ClickHouse; optional DuckDB follows
the same catalog contract without changing the default CGO-free binary.

All new commands and file formats below are proposals, not current interfaces.

## Core and managed-host ownership

Core owns the exact-object catalog capability, a versioned portable evidence
snapshot, the deterministic offline generator, and local CLI composition. These
are reusable developer tools, not a managed authoring store or a second semantic
authority. A managed host such as Metis Cloud may call the same Core operations
behind its own source-selection, authorization, editor, and review surfaces. It
must bind the inspected Project and DataSource to its own stable identities and
deployment revision before using the evidence. Cloud's Draft, Candidate,
Release, and Deployment lifecycle remains Cloud-owned; this RFC adds no Core
publication state, Cloud route, or automatic activation.

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
metis source inspect --config ./metis.yaml --project sales \
  --data-source warehouse --relations ./authoring/relations.json \
  --output ./authoring/catalog.json

# New offline command: reproducible from checked-in inputs.
metis project init --catalog ./authoring/catalog.json \
  --mapping ./authoring/model-map.yaml --output ./candidate-sales

# Existing validation command; optional online validation is RFC-0088.
metis project validate --project sales --config ./candidate-sales/project.yaml
```

`metis source inspect` can run before a semantic model exists. Given the explicit
Project ID, the authoring service first authorizes Project `author`, before it
consults Project registration, DataSource/Backend inventory, secrets, or catalog
state. A denied request performs none of those lookups or online operations and
does not reveal whether the selected DataSource exists or is applied. After
authorization, the command loads deployment configuration and the selected
registration without bootstrapping semantic sources, verifies that the named
DataSource is applied to that Project, and uses bounded Runner catalog inspection.
This is a deliberate authoring-only assembly path. Runtime serve/bootstrap rules
remain unchanged and still require valid semantic sources.

Local execution is trusted authoring under the operator's OS/database identity.
There is no REST/MCP route or Agent-controlled catalog tool in this version.
An embedder must additionally enforce any host-specific permission to inspect
physical DataSource metadata; Project `author` does not override a stricter host
policy. Both decisions precede secret resolution and inspection. Query visibility
permissions alone do not authorize catalog inventory. A managed host must not
reuse the trusted-local all-access policy for remote callers.

The relation selector file uses an explicit list of structured names:

```json
{
  "schema_version": 1,
  "relations": [
    {"id": "orders", "parts": ["sales", "orders"]}
  ]
}
```

Identifier-part meaning follows the selected backend; the example is illustrative
and does not prescribe three-part names for every engine. Wildcards, full-database
crawling, user SQL, views' SQL definitions, and row sampling are excluded.
Relation IDs must be unique within the selector and remain stable aliases for
the exact structured references; they are not inferred from table names.

### Shared catalog contract

Reuse RFC-0088's exact-object `CatalogInspector` capability and Runner lifecycle.
Only that small primitive is a prerequisite; the complete online-validation CLI
is not a dependency of the offline generator. Do not introduce another connection
profile, pool, secret resolver, SQL executor, or physical placement authority.

Catalog schema version 1 binds the Project ID and logical DataSource name selected
by `metis source inspect` to the backend family. Every inspected relation retains the
unique selector ID, requested structured identifier parts, resolved physical
identity when available, a closed inspection outcome, and whether its column
inventory is complete. Found relations contain column identifiers, native types
with precision/scale/timezone evidence, nullability as known/unknown, and key
metadata with its actual backend meaning. The command rejects missing, duplicate,
or extra per-relation results rather than silently changing a selector's target.
Only an all-found snapshot with complete column inventories is a successful
generator input; other outcomes produce an inspection failure report, not a
partially successful catalog for offline generation.

The digest covers normalized stable content, including Project, logical source,
selector IDs and ordered identifier parts; observation time and server-version
observations are separate metadata. Stable normalization sorts unordered backend
responses without losing physical case, part boundaries, or backend distinctions.
The snapshot is database evidence, not semantic authority, a permission token, or
a guarantee that a future operation reaches the same database instance. It must
not contain credentials, endpoints, row values, source view SQL, or automatic
samples. Column comments are excluded initially. A managed host may retain its
own opaque source ID and deployment revision alongside the Core snapshot, but
must not put host-specific identity or secrets into Ossie.

Limits match the shared inspection facility: at most 200 selected relations,
10,000 columns, 10 MiB metadata, a 5-minute total deadline and a 30-second
per-operation deadline, tightened by deployment ceilings. Missing permissions,
unreadable relations, and overflow produce a failure report rather than a silently
partial successful catalog. No background synchronization is introduced.
Successful metadata inspection proves only the facts the backend actually
observed; it does not establish SELECT permission on the relation or guarantee
that a subsequent semantic query will succeed. A privilege error is never
reported as a missing relation.

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
Each `datasets[].relation` resolves exactly one snapshot selector ID, not a
similarly named physical object. The map's `project` must equal the snapshot
Project. If `data_source` is present, it must equal the snapshot's logical
DataSource or generation fails; it cannot redirect inspected metadata to a
different source. The field is an optional assertion, not a placement override:
the generator always emits the snapshot's logical DataSource name in the existing
model-level placement extension. These checks do not turn the snapshot into
runtime placement authority: a later deployment may bind the same logical name
differently and must be validated separately.

The first version does not generate join relationships. Observed foreign-key or
key metadata may be included in a separate author review report, with provenance.
Doris key-model declarations and ClickHouse primary/sorting keys must not be
translated into unique semantic keys or many-to-one cardinality guarantees.
An author adds and validates semantic relationships after reviewing the facts.

### Deterministic generation

For identical snapshot bytes, authoring map, and generator version, output bytes
are identical. Output-directory names and staging paths must not enter generated
bytes or digests. Use Metis's supported Ossie version and canonical source formatter.
Store generation provenance and pending review items in a sidecar, not invented
semantic fields or compiler-interpreted prose. Preserve physical case and quoting
where the current Ossie source grammar and Renderer can do so losslessly.

Logical names must follow the current loader's grammar. Authors provide explicit
names; invalid/reserved names and collisions are reported with suggested repairs,
never resolved by silently renaming. Relative output paths cannot escape the output
directory or follow symlinks into another project.

Relation inspection accepts structured identifier parts, but the current Ossie
dataset `source` is a dotted string and the built-in Renderers split it on `.`
before quoting each part. V1 generation must reject any inspected relation whose
parts cannot be represented and rendered unambiguously, including a part that
itself contains `.`, with a stable unsupported-identifier finding and no
candidate directory. Supplying pre-quoted text is not a workaround. Extending
the source grammar and Renderer contract is separate work. Tests must cover
quoted, mixed-case, reserved, and delimiter-bearing identifiers for both initial
backends; no candidate may silently target a different physical relation.

Type mappings are backend-specific and tested. Preserve Decimal versus Float and
Date versus timestamp distinctions. A native type that cannot be mapped faithfully
requires explicit column exclusion or an authored supported mapping. Unknown
types are not silently coerced to String, and lossy casts are not generated.
Generate dialect-specific direct-column expressions when ANSI syntax cannot
represent the physical identifier unambiguously.

The model-level METIS placement extension contains only the snapshot's logical
source name; connection configuration and credentials remain outside Ossie.
Generation operates on one source snapshot per invocation and produces no
cross-source relationship.

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
Structural failure prevents exposing the candidate directory. Advisory quality
warnings remain visible. Successful generation means a structurally valid candidate
was produced, even when authoring is incomplete; it never means ready to deploy.
`GETTING_STARTED.md` lists the exact next commands and registration requirements.
The report directs authors to run optional RFC-0088 online validation against
the eventual target, followed by an explicitly authorized test query if they
need evidence that its execution role can read data. Neither step is performed
implicitly by `metis project init`.

### File and error behavior

Refuse an existing output directory; V1 has no force/merge/in-place update mode.
Stage all files privately on the same filesystem, validate them, then atomically
rename the directory. A failure leaves no advertised partial project. Use
owner-only file permissions. Re-running into a different directory and using
the existing `metis project diff` is the supported review workflow.

Unknown schema versions/fields, invalid mappings, identifier collisions, missing
selected relations/columns, and unsupported types have stable authoring diagnostic
codes with file/field locations. Mapping/model repairs use `CHANGE_MODEL`;
inspection capability or credentials use `CHANGE_TARGET`. Native errors and
secret values are redacted centrally. Do not derive actions by parsing messages.

Generation exits 0 on a validated candidate with a complete report, 1 on a
generation/inspection finding that prevents output, and 2 on command/input/I/O
failure. Pending author review is visible in the report and does not silently
activate the candidate. Normal `metis project validate` eligibility semantics still
apply before an author adopts it.

A managed host may import generated files into its existing mutable Draft or
immutable Candidate path only after an explicit author action. Its Project and
DataSource permissions, concurrency checks, review, publication approval, and
Deployment activation are separate from Core generation. It must not treat a
Core validation result or catalog digest as a host publication approval.

## Alternatives

- Infer business metrics and relationships with an LLM: useful as a separate
  author aid later, but it cannot be the authority for deterministic generation.
- Convert every numeric column to SUM: produces incorrect metrics for identifiers,
  balances, prices, ratios, and snapshots.
- Expose all columns as dimensions: bypasses deliberate semantic publication and
  may disclose technical or sensitive fields. Require an explicit selection.
- Import external semantic-model formats in the first release: expands the
  compatibility problem before the native authoring workflow is proven. Future
  importers should feed the same reviewed Ossie candidate boundary with explicit
  loss reports.

## Rollout and migration

1. Implement offline `metis project init` from versioned catalog fixtures and explicit
   maps; it can ship before online inspection.
2. Reuse the shared catalog capability for Doris/ClickHouse `metis source inspect`.
3. Add complete Doris and ClickHouse tutorials using small disposable datasets,
   environment-based credentials, a first business metric, compilation, and query.
4. Verify optional DuckDB support in its existing build flavor.

Existing authored documents are never regenerated or overwritten. Adoption is
manual through normal source control and existing runtime registration/activation.
The CLI creates no database objects, Git repository, Cloud release, or deployment.
Managed authoring clients consume the Core result through their existing Draft
and review lifecycle; this RFC does not specify their UI or persistence APIs.

## Test and acceptance criteria

- Reordered catalog responses produce the same normalized snapshot and output.
- Duplicate selector IDs and missing/duplicate/extra driver results fail closed.
  The successful snapshot retains each selector ID, exact identifier-part
  boundaries, Project, logical DataSource, backend family, and stable digest.
- A map targeting another Project, unknown selector ID, or different logical
  DataSource fails without output. Whether the map repeats `data_source` or not,
  output placement equals the snapshot's logical source and cannot silently
  select another route.
- Reserved names, case-sensitive identifiers, Decimal precision, temporal types,
  nullable fields, unsupported native types, and duplicate names are discriminating
  test fixtures; generated output passes the current loader and formatter.
- Relation identifier parts containing `.` or otherwise not representable by
  current source quoting fail with an actionable finding; pre-quoting cannot
  bypass this guard. The test checks rendered physical identity, not just YAML
  validity. Column names with unusual quoting either round-trip or fail closed.
- Repeated generation into different directories from identical inputs yields
  byte-identical files and digests, including the report and getting-started text.
- Generation never invents metric business meaning or semantic uniqueness from
  ClickHouse sorting/primary keys or Doris key-model metadata.
- A selected subset cannot leak unselected column definitions into model output.
- Fresh generation works before a Project has valid semantic sources; serving
  that same incomplete Project still fails existing runtime bootstrap validation.
- Online inspection shares production admission, secrets, cancellation, and cleanup;
  no source row SELECT is performed by the catalog path.
- Denied Project `author` or host DataSource-inspection authority performs no
  registration/Backend read, secret resolution, or catalog operation and reveals
  no selected-source existence. Metadata success makes no claim about later
  SELECT permission or execution success; ambiguous privilege errors are not
  reported as missing objects.
- A managed host can import a generated candidate only under its own authoring
  and revision policy; generation cannot publish or activate a Release.
- Failure preserves any existing directory and does not expose partial output.
- An unfamiliar developer can follow each checked-in backend tutorial from
  physical tables to one validated and executed semantic query. A suggested
  usability target is 30 minutes after the database is reachable, measured and
  reported as a target rather than assumed achieved.

## Documentation updates

Update the source-authoring specification, CLI reference, Renderer/Driver extension
guidance, example index, and add dedicated Doris/ClickHouse onboarding guides at
implementation time. Keep this Draft linked in the RFC index during review.

## References and review decisions

- [Source authoring contract](../../specs/semantic/asset-authoring-lifecycle.md)
- [Model quality diagnostics](../../specs/semantic/model-quality-diagnostics.md)
- [Runtime placement](../../specs/operations/runtime-bootstrap.md)
- [Current Ossie validation](../../../ossie/validate.go)
- [Current sales example](../../../examples/demo/models/sales.ossie.yaml)

The first release includes `row_count` only when the author explicitly asks for
it; the safe default remains no generated metrics. Backend type mappings and
identifier handling must pass the initial-engine conformance fixtures before
implementation is considered complete. The authoring map is tooling input and
must not grow into a second runtime semantic schema.
