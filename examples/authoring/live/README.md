# Doris and ClickHouse: from a table to a semantic query

This walkthrough uses three disposable rows. It exercises the installed `metis`
CLI, not repository-only application APIs. Start after a development database is
reachable. The same workflow supports Doris and ClickHouse; only setup SQL,
connection variables and the compile dialect differ.

Prerequisites: `metis` (the default build includes both backends), Bash, curl, openssl,
and the database's SQL client. From a checkout, build with `make metis-build`
and set `METIS_BIN` to the **absolute** path of `bin/metis`:

```sh
export METIS_BIN="$PWD/bin/metis"
```

For an already installed binary, use `export METIS_BIN=metis` instead.

Run the following commands from the repository root. Database setup is explicit
and external: neither `catalog inspect` nor `project init` creates or changes
database objects. Use a disposable development warehouse, never production.

## 1. Prepare one fresh dataset

Choose a backend. Use an account allowed to create the demo database/table for
setup, and a separate read-only identity for Metis where practical. Set passwords
through your secret manager or an interactive shell prompt; do not put values in
the YAML files, SQL, Git, command arguments or shell history. The checked-in
configuration contains environment references only.

### Doris

```sh
export METIS_DORIS_HOST=127.0.0.1
export METIS_DORIS_PORT=9030
export METIS_DORIS_USERNAME=metis_reader
export METIS_SETUP_USERNAME=your_setup_account
```

Load [setup-doris.sql](setup-doris.sql) once, entering the setup account's
password when prompted:

```sh
mysql --host "$METIS_DORIS_HOST" --port "$METIS_DORIS_PORT" \
  --user "$METIS_SETUP_USERNAME" -p < examples/authoring/live/setup-doris.sql
```

Set `METIS_DORIS_PASSWORD` for the Metis identity via your secret manager or a
private prompt. Grant it metadata inspection and SELECT access to the demo
relation using your existing database administration process.

### ClickHouse

```sh
export METIS_CLICKHOUSE_ADDRESS=127.0.0.1:8123
export METIS_CLICKHOUSE_USERNAME=metis_reader
export METIS_SETUP_USERNAME=your_setup_account
```

Load [setup-clickhouse.sql](setup-clickhouse.sql) once with a configured SQL
client. For a local native-protocol client, use port 9000 (not the HTTP 8123
endpoint used by Metis) and an interactive password prompt:

```sh
clickhouse-client --host 127.0.0.1 --port 9000 \
  --user "$METIS_SETUP_USERNAME" --ask-password --multiquery \
  < examples/authoring/live/setup-clickhouse.sql
```

Set `METIS_CLICKHOUSE_PASSWORD` for the Metis identity separately. Grant that
identity metadata inspection and SELECT access to the demo relation. The local
example uses HTTP; configure `scheme: https` and the correct endpoint when TLS is
required by your environment.

Both setup files create `metis_authoring_demo.orders` with these known values:

| region | amount |
| --- | --- |
| APAC | 10.25 |
| APAC | NULL |
| EMEA | 7.50 |

They also include `order_time` and a deliberately unpublished `internal_note`.
Existing databases are refused; no DROP, TRUNCATE or replacement is performed.
Stop if setup fails rather than continuing with unknown or repeatedly inserted
data. Freeze writes while checking results.

## 2. Capture and generate

For Doris:

```sh
bash examples/authoring/live/prepare.sh doris ./demo-doris
```

For ClickHouse:

```sh
bash examples/authoring/live/prepare.sh clickhouse ./demo-clickhouse
```

Choose one and use a fresh work directory. The script copies reference-only
configuration privately, runs `catalog inspect`, `project init`, `project validate`
and `query compile`. It performs no setup SQL, grants, row sampling or automatic
business-model adoption. It works before `candidate-sales/project.yaml` exists.
The generated model selects three columns, omits `internal_note`, and contains
only the explicitly requested technical `order_rows = COUNT(*)` metric.

Set your chosen work directory for the remaining commands:

```sh
DEMO=./demo-doris
# For ClickHouse instead: DEMO=./demo-clickhouse
```

Inspect `catalog.json`, `candidate-sales/authoring-report.json`, the generated
model and `GETTING_STARTED.md`. Metadata success does not prove SELECT access.
The report records generation evidence; it does not approve later model edits.

## 3. Review and author the business definition

[model-reviewed.ossie.yaml](model-reviewed.ossie.yaml) is a **manually authored**
example for this exact toy table, not generated inference. Its `total_revenue`
means the sum of recorded `amount` values in one currency. NULL amounts are
omitted by SUM, not silently replaced with zero. It makes no claim about refunds,
tax, currency conversion, entity uniqueness, or production revenue recognition.
`order_rows` counts physical rows, including the row with NULL amount. No time
dimension, timezone/calendar semantics, join or uniqueness is inferred.

Compare the reviewed file with the generated model and read those definitions.
Only after accepting them for the demo, explicitly replace the model in your
fresh candidate; the generator itself never overwrites it:

```sh
cp "$DEMO/model-reviewed.ossie.yaml" "$DEMO/candidate-sales/models/sales.ossie.yaml"
"$METIS_BIN" project validate --project sales --config "$DEMO/candidate-sales/project.yaml"
"$METIS_BIN" query compile --model "$DEMO/candidate-sales/models/sales.ossie.yaml" \
  --dialect DORIS --metric order_rows --metric total_revenue --dimension region \
  --output "$DEMO/reviewed-query.json"
```

For ClickHouse, use `--dialect CLICKHOUSE`. Compilation checks the model and
produces SQL without executing it. Keep the original generation report as
provenance, and validate/review edits separately before any deployment.

## 4. Validate against the configured engine, then check results

```sh
"$METIS_BIN" project validate --online --project sales --config "$DEMO/metis.yaml" \
  --queries "$DEMO/queries.json" --output "$DEMO/validation.json"
```

Default validation is offline; `--config` selects the semantic project manifest.
Online selects the deployment root, prepares every case before connecting,
inspects required columns/type families and runs EXPLAIN. It does not run the
compiled SELECT. The report marks completed stages. Parameterized queries are
unsupported in this version for both engines; this example has no parameters.
Accepted plans do not certify values or guarantee a later SELECT.
Always use fresh report paths.

```sh
"$METIS_BIN" project test --mode runtime --project sales --config "$DEMO/metis.yaml" \
  --suite "$DEMO/results.yaml" --output "$DEMO/results.json"
```

The suite invokes the normal query service and checks handwritten expectations,
not values captured from the implementation. APAC has two physical rows and
revenue 10.25; EMEA has one row and revenue 7.50. Row order is immaterial. Decimal
values are compared exactly. The suite does not provision fixtures or test custom
host authorization, and its report excludes raw result values.

## 5. Query through the authenticated REST interface

Generate a private local token and start the server in this shell:

```sh
export METIS_API_KEY="$(openssl rand -hex 32)"
"$METIS_BIN" serve --config "$DEMO/metis.yaml" --addr 127.0.0.1:8080
```

In another shell, set the same token privately, check readiness, then run the
checked-in [semantic request](query.json):

```sh
curl -fsS http://127.0.0.1:8080/readyz
bash examples/authoring/live/request.sh http://127.0.0.1:8080
```

The response schema is `region`, `order_rows`, `total_revenue`, with rows
equivalent to `["APAC", 2, "10.25"]` and `["EMEA", 1, "7.5"]`. Decimal string
trailing zeros may differ; row order is unspecified. The request supplies semantic
intent, not a database endpoint, dialect or arbitrary SQL. The token travels on
stdin to curl, and the helper accepts only a local HTTP endpoint without redirects.
This static token grants trusted-local access; it is not a managed-host policy.

Stop the server with Ctrl-C. Keep your reports for review; do not rerun generation
into an existing work directory. Remove the disposable database only through your
normal database administration process after confirming that you created it for
this exercise. Nothing here automatically deletes database or project data.

## Troubleshooting and verification

- Connection/configuration failure: check the selected backend, environment
  references, reachable address and port. ClickHouse execution uses HTTP(S), not
  its native client port. Do not add plaintext credential fallbacks.
- Metadata succeeds but query fails: inspect SELECT privileges and model/source
  binding. Catalog success does not bypass database authorization.
- Wrong counts: confirm a fresh fixture and no repeated inserts or concurrent
  writes; do not approve a new baseline to hide fixture drift.
- Existing output: choose a new directory/report path; the walkthrough does not
  force replacement or regenerate an authored model.

The existing real-engine matrix executes these setup files, the actual `prepare.sh`
CLI commands, explicit reviewed-model adoption, validation/compilation, runtime
suite, server startup and authenticated `request.sh` for both backends. Default
checks validate the checked-in model/map/suite and script safety without requiring
live warehouses. A 30-minute human walkthrough is a usability target, not a
measured claim; database provisioning and secret setup are outside that target.
