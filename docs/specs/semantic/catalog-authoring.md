# Catalog-assisted authoring

`metis project init` creates a local review candidate from a versioned catalog
snapshot and an explicit authoring map. It reuses Ossie formatting, project
loading, and quality checks. It works without a running server, database
connection, secrets, or repository test files.

```sh
metis project init --catalog ./catalog.json --mapping ./model-map.yaml \
  --output ./candidate-sales
metis project validate --project sales --config ./candidate-sales/project.yaml
```

The [examples](../../../examples/authoring/README.md) provide synthetic Doris and
ClickHouse snapshots and an online capture walkthrough. The generated expressions are specific to
the snapshot's backend; unsupported dialect compilation fails explicitly.

The [live table-to-query walkthrough](../../../examples/authoring/live/README.md)
adds disposable setup SQL, explicit reviewed business definitions, exact result
expectations and authenticated querying. It is tested through the real CLI for
both backends. Setup and manual model adoption stay outside generation; a
generation report is provenance, not approval of later edits.

## Online metadata capture

```sh
metis catalog inspect --config ./metis.yaml --project sales \
  --data-source warehouse --relations ./relations.json --output ./catalog.json
```

All five flags are required; positional arguments are refused. The selector is
strict JSON or YAML, at most 1 MiB, with `schema_version: 1` and 1–200 `relations`:

```json
{"schema_version":1,"relations":[{"id":"orders","parts":["Analytics","Orders"]}]}
```

IDs and requested physical identities must be unique. Online references require
exact `database.table` parts for ClickHouse and `database.table` or
`catalog.database.table` parts for Doris. No default database, wildcard, SQL,
view definition, row sample, default expression or column comment is exported.
Physical identifiers retain case and spaces; dots within parts, prequoted names,
quotes, backslashes, control characters and SQL separators are rejected. Full
column inventories are captured for selected relations; the authoring map later
chooses which columns become semantic fields. V1 does not collect keys, because
column-level key flags cannot establish their ordering or physical key model.

`bootstrap.LoadCatalogRunner` evaluates Project `author`, then a required host
physical metadata access callback, before reading deployment/registry files.
Omitted policies fail closed. Only after both allow does it validate the selected
source's Project registration, Backend configuration and execution ceilings.
The registered project manifest path need not exist yet and is never loaded.
Credentials and connections are acquired lazily through the existing Runner.
Trusted-local CLI execution explicitly opts into local Project and catalog access
under the operator's OS/database identity. This is not remote authorization:
embedders must supply their own policies, and no REST/MCP route is added.

`driver.CatalogInspector` is an optional execution-lease capability. Doris uses
exact-object `DESCRIBE`; ClickHouse uses `DESCRIBE TABLE` with subcolumns disabled.
The Runner shares admission, secret resolution, process pools, shutdown and
cleanup. Work is bounded to five minutes overall, 30 seconds per open/describe,
10,000 columns total and 10 MiB metadata, further tightened by deployment/host
ceilings. Unknown driver errors, empty inventories, interrupted streams,
duplicate columns and mismatched/partial results fail without a catalog file.
A privilege error is never inferred to mean a missing table. Inspection proves
only metadata evidence, not SELECT permission or future query success.

Output is normalized JSON with the existing digest, privately staged and
atomically published to a new file (0600). Existing files, directories and
symlinks are refused, including concurrent creation. Parent directories must
exist; no overwrite flag is available. No credential, endpoint, raw database
error, observation SQL or business inference enters the snapshot.
Exit 0 means a complete snapshot exists; exit 1 means authorization, configuration,
inspection or cleanup failed; exit 2 means malformed selectors, CLI arguments or
output failure. Diagnostics use fixed messages and stable codes; unknown errors
use `CATALOG_FAILED`. Failed inspection leaves output absent, not a partial
snapshot or a failure document masquerading as generator input.

## Catalog schema version 1

Catalog inputs are regular files containing strict JSON or YAML, at most 10 MiB, with:

- `schema_version: 1`, `project`, `data_source`, and `backend` (`doris` or `clickhouse`).
- `relations`: 1 through 200 entries, at most 10,000 columns total. Each has a
  unique selector `id`, ordered physical identifier `parts`, optional
  `resolved_parts`, `outcome: found`, `columns_complete: true`, and a nonempty
  complete `columns` inventory. Duplicate requested physical identities fail.
- Columns have exact case-sensitive `name`, `native_type`, and `nullable`
  (`nullable`, `not_null`, or `unknown`). A native type contains a base `name`
  and optional integer `precision`, `scale`, `length`, and string `timezone`.
  Decimal precision/scale and timestamp fractional precision are separate
  evidence according to the native type. Wrappers such as `Nullable(Int64)`
  must be represented as base `Int64` with separate nullability evidence.
- Optional `keys` entries have `kind` (`primary`, `unique`, `sorting`, `duplicate`,
  or `aggregate`) and ordered `columns` referencing the inventory. These retain
  observed physical meaning and never generate semantic keys or relationships.
- Required `digest` is SHA-256 of the normalized stable snapshot; optional
  `observation` contains `observed_at` and `server_version` and is excluded from it.

`authoring.CatalogDigest` is the producer-facing digest helper. It copies the
typed snapshot, sets `digest` to an empty string, omits `observation`, sorts
relations by ID, columns by name, and keys by their canonical JSON bytes, then
hashes its compact JSON encoding. Identifier-part and key-column order, physical
case, Project, logical source, native evidence and backend remain significant.
Reordered inventory responses produce the same digest. This is an integrity
check, not authentication or proof of current database state. The generator
verifies it before creating a candidate.

Unknown fields (including credentials, endpoints, row samples and SQL), duplicate
keys, multiple documents, custom tags, YAML anchors/aliases, unsupported versions,
and oversized inputs fail explicitly. A partial or non-found relation fails even
when the map does not select it; incomplete snapshots are not generation inputs.

## Authoring map

The map is strict JSON or YAML, at most 1 MiB. Its schema is:

```yaml
schema_version: 1
project: sales
model: sales
data_source: warehouse
datasets:
  - relation: orders
    name: orders
    fields:
      - {column: Region, name: region, dimension: true}
      - {column: Amount, name: amount}
starter_metrics:
  - {name: order_rows, kind: row_count, dataset: orders}
```

Project must match the snapshot. Optional `data_source` asserts the snapshot's
logical source; omitting it does not change placement. Every selected relation
ID and column must exist exactly. Only selected fields are emitted. Dataset
selector IDs and names are unique; field names and physical selections are unique
within each dataset. Model, dataset, field and metric names use an ASCII letter/underscore followed by
letters, digits or underscores, up to 64 characters. SQL keyword and portable
filesystem reserved names are rejected with a repair suggestion. Names that
collide case-insensitively also fail rather than being silently renamed. The
Project ID retains its existing source-loader identity contract and is quoted
as a shell argument in generated next-step commands.

`starter_metrics` is optional, with at most 16 entries. Only explicit technical `row_count` is supported;
its `COUNT(*)` includes NULL-containing rows and means physical rows, not distinct
orders or another business entity. The initial implementation requires a single
selected dataset when generating row counts, keeping its source binding
unambiguous. Multiple selected datasets are supported for metric-free skeletons.
No SUM, average, business formula, join, time-dimension flag, uniqueness, or
cardinality is inferred. Authors add those decisions after review.

## Types and physical identity

The generator never inserts casts. Supported native type families are:

| Backend | Native types | Ossie datatype |
| --- | --- | --- |
| Doris | CHAR, VARCHAR, STRING | String |
| Doris | BOOLEAN, BOOL | Boolean |
| Doris | TINYINT, SMALLINT, INT, INTEGER, BIGINT, LARGEINT | Integer |
| Doris | FLOAT, DOUBLE | Float |
| Doris | DECIMAL, DECIMALV2, DECIMALV3 with explicit precision/scale | Decimal |
| Doris | DATE, DATEV2 | Date |
| Doris | DATETIME, DATETIMEV2 with optional fractional precision 0–6 | DateTime |
| ClickHouse | String, FixedString | String |
| ClickHouse | Bool | Boolean |
| ClickHouse | Int/UInt 8, 16, 32, 64, 128, 256 | Integer |
| ClickHouse | Float32, Float64 | Float |
| ClickHouse | Decimal with explicit precision/scale | Decimal |
| ClickHouse | Date, Date32 | Date |
| ClickHouse | DateTime, DateTime64 with optional fractional precision 0–9 | DateTime, or DateTimeTz with explicit timezone evidence |

The initial Decimal subset requires precision 1–38 and scale 0–precision.
Unsupported types or contradictory evidence require explicit column exclusion;
unselected unsupported columns do not prevent generation. Native precision,
scale, length, nullability and timezone evidence are preserved in the sidecar
report. They are not invented Ossie fields. Native type names do not prove a
server-version capability; this stage has performed no database inspection.

For physical sources, resolved identifier parts are used when provided, otherwise
requested parts are used. The source retains case and part order. ClickHouse
requires exactly `database.table`; Doris supports one through three parts. Dots
inside a part, pre-quoted text, embedded quote/backslash characters, control
characters and leading/trailing whitespace fail rather than changing identity.
The same restrictions apply to column identifiers, while spaces, reserved native
names and mixed case are retained in backend-specific quoted field expressions.
Tests verify resulting renderer output, not merely YAML validity.

## Files, validation, and review

Output is a new directory containing:

```text
project.yaml
models/<model>.ossie.yaml
authoring-report.json
GETTING_STARTED.md
```

The existing model-level METIS placement extension contains the snapshot's
logical DataSource name. Connection configuration stays outside the model.
The report includes catalog and mapping digests, generator version, selected
physical mappings and native evidence, observed keys, review tasks, and existing
validation/quality results. Mapping normalization sorts datasets, fields and
metrics by logical name before hashing and generation. Observation timestamps,
staging paths and output-directory names never enter generated bytes.

A metric-free candidate is valid authoring output. Review business metrics,
timezone/calendar conventions, null handling, and relationship cardinality.
`validation.publishable` retains its existing quality-threshold meaning; it is
not approval to deploy. Register the Project and logical DataSource in the
eventual deployment, validate the candidate, and perform an authorized query
when execution evidence is needed.

Files are staged privately on the destination filesystem, formatted and checked
through the shared loader, then published with an atomic no-replace directory
rename. Supported release platforms are Linux and macOS; an unavailable
filesystem primitive fails without publishing. Existing files/directories and
symlinks are always refused, including a destination created concurrently.
There is no force, merge, or overwrite option. Files are owner-only (0600) and
directories private (0700); failure cleans this invocation's staging directory.
The parent must already exist. Generate into another directory to compare changes
using the existing `metis project diff` workflow.

Exit 0 means a validated local candidate exists with its report. Exit 1 means
an authoring finding prevents generation; exit 2 means malformed input, command
or I/O failure. Findings have stable `AUTHORING_*` codes, structured locations,
fixed messages and `CHANGE_MODEL` (`CHANGE_TARGET` for an unsupported backend);
raw parser and filesystem errors are excluded.
Pending review tasks do not cause an error or any automatic activation.
