# Generate a reviewable project offline

These snapshots are synthetic metadata examples, not captured database evidence.
They describe the same physical `Analytics.Orders` relation using Doris or
ClickHouse native type evidence. Their normalized digests are checked by the
generator; no database is needed for this walkthrough.

From the repository root after building `bin/metis`:

```sh
bin/metis project init --catalog examples/authoring/catalog-doris.json \
  --mapping examples/authoring/model-map.yaml --output ./candidate-doris
bin/metis project validate --project sales --config ./candidate-doris/project.yaml

bin/metis project init --catalog examples/authoring/catalog-clickhouse.json \
  --mapping examples/authoring/model-map.yaml --output ./candidate-clickhouse
bin/metis project validate --project sales --config ./candidate-clickhouse/project.yaml
```

`model-map.yaml` deliberately selects three fields and omits `InternalNote`.
It explicitly requests `order_rows`, a technical `COUNT(*)` of physical rows.
NULL values in any field do not remove rows from this count. No sum of Amount,
time semantics, unique key, relationship, or business definition is invented.
Remove `starter_metrics` to generate a metric-free skeleton.

Review `authoring-report.json` and `GETTING_STARTED.md` in each candidate. The
snapshots' duplicate/sorting keys are recorded as physical hints, not semantic
uniqueness. The model binds the logical source `warehouse`; its eventual
connection and credentials must be configured independently in deployment.
Generation has not tested database permissions or execution.

Compile the explicit row count for the corresponding backend:

```sh
bin/metis query compile --model candidate-doris/models/sales.ossie.yaml \
  --dialect DORIS --metric order_rows --dimension region
bin/metis query compile --model candidate-clickhouse/models/sales.ossie.yaml \
  --dialect CLICKHOUSE --metric order_rows --dimension region
```

Existing candidate directories are refused. For another iteration, use a fresh
directory and review the model/report diffs before adopting them. The CLI works
outside this repository too: pass your own catalog and map paths with an
installed `metis` binary. Online capture is optional, not required for offline
generation.

## Capture live metadata

Use your own deployment or copy this directory's `metis.yaml` and a DataSource
file to a private working directory. For ClickHouse, change the registry path to
`./datasources-clickhouse.yaml`. Set the referenced environment variables in your
shell; never put passwords into the files or command arguments. Use your real
host/port or HTTP address and a database identity allowed to describe the table.
The project manifest named in this authoring registration need not exist yet.

`relations.json` explicitly selects `Analytics.Orders`. Adjust its structured
parts and the map's column selections to your actual table; the supplied snapshots
are synthetic and do not create that table for you. Doris also accepts explicit
three-part `catalog.database.table` selectors. ClickHouse requires two parts.

```sh
metis catalog inspect --config ./metis.yaml --project sales \
  --data-source warehouse --relations ./relations.json --output ./catalog.json
metis project init --catalog ./catalog.json --mapping ./model-map.yaml \
  --output ./candidate-sales
metis project validate --project sales --config ./candidate-sales/project.yaml
```

Only the selected table's column metadata is read; no row data, database crawl,
comments or inferred business metrics are captured. The map emits only selected
columns. Unsupported native types remain evidence and must be excluded explicitly
if the generator cannot represent them. Inspection errors are redacted and do
not publish partial snapshots. Use a fresh output file for each capture.
Successful inspection does not prove that subsequent SELECT queries are allowed.
After generation, update the deployment's project path to your candidate, review
the report, and use the normal deployment validation/query workflow.

See the [shipped schema and limits](../../docs/specs/semantic/catalog-authoring.md)
and [RFC-0089](../../docs/proposals/tooling/0089-catalog-assisted-ossie-authoring.md).
