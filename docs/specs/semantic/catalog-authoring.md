# Offline catalog-assisted authoring

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
ClickHouse snapshots. Online `metis catalog inspect` is a proposed follow-up
command and is not implemented yet. The generated expressions are specific to
the snapshot's backend; unsupported dialect compilation fails explicitly.

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
