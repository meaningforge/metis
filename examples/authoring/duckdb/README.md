# DuckDB: from a local table to a semantic query

This sample uses an existing disposable DuckDB file and the optional DuckDB-enabled
Metis build. Metis opens the file read-only. Database provisioning remains an
independent DuckDB CLI action. No Docker or database server is required.

Build from the repository root (Go, CGO and a C/C++ toolchain are required):

```sh
CGO_ENABLED=1 go build -tags duckdb -o bin/metis ./cmd/metis
export METIS_BIN="$PWD/bin/metis"
```

The standard build has no executable DuckDB backend. Its offline generator can
consume DuckDB snapshots, but capture, online validation and runtime tests require
the optional build. The existing `make test-duckdb-backend` gate verifies it.

Install the DuckDB CLI independently, then seed a fresh disposable database:

```sh
DEMO_ROOT="$(mktemp -d)"
duckdb "$DEMO_ROOT/demo.duckdb" < examples/authoring/duckdb/setup.sql
export METIS_DUCKDB_PATH="$DEMO_ROOT/demo.duckdb"
bash examples/authoring/duckdb/prepare.sh "$METIS_DUCKDB_PATH" "$DEMO_ROOT/work"
DEMO="$DEMO_ROOT/work"
```

The script captures exactly `main.orders` and generates an offline candidate from
explicit field mappings. The snapshot includes the unpublished column as metadata;
the model excludes it. Only the technical row count is generated automatically.
Inspect `catalog.json`, the candidate and `authoring-report.json`. Three-part
catalog/schema/table selectors are supported when explicitly provided; no default
schema/database is inferred. Complex native types stay explicit unsupported evidence.

Review the handwritten `total_revenue`: this toy example uses one currency, omits
NULL amounts, and introduces no inferred calendar, joins or business definitions.
After accepting it for this disposable sample, explicitly adopt it:

```sh
cp "$DEMO/model-reviewed.ossie.yaml" "$DEMO/candidate-sales/models/sales.ossie.yaml"
"$METIS_BIN" semantic validate --offline --project sales --config "$DEMO/candidate-sales/project.yaml"
"$METIS_BIN" semantic validate --online --project sales --config "$DEMO/metis.yaml" \
  --queries "$DEMO/queries-filtered.json" --output "$DEMO/validation.json"
"$METIS_BIN" semantic test --mode runtime --project sales --config "$DEMO/metis.yaml" \
  --suite "$DEMO/results.yaml" --output "$DEMO/results.json"
```

Online validation binds the original ordered parameters to ordinary EXPLAIN; it
does not execute the compiled SELECT. It reports catalog/type-family and planning
coverage, not numerical correctness, precision/timezone certification or future
database state. Runtime tests separately assert the handwritten results: APAC has
two rows and revenue 10.25; EMEA has one row and revenue 7.50. All report paths must
be fresh. The original generation report records provenance, not approval of edits.

DuckDB identifier matching is ASCII case-insensitive; native spelling is preserved
in snapshots. Filesystem read permission replaces remote database credentials,
and host/project authorization still precedes configuration and file opening.
Stop independent writers before opening the production read-only connection.
No database mutation or fixture provisioning command is added to Metis.
