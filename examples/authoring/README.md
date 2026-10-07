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
installed `metis` binary. The online `metis catalog inspect` command is a future
stage, not required for offline generation.

See the [shipped schema and limits](../../docs/specs/semantic/catalog-authoring.md)
and [RFC-0089](../../docs/proposals/tooling/0089-catalog-assisted-ossie-authoring.md).
