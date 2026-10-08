# Runtime metric regression fixture

This small fixture has manually reviewed expectations: APAC is `50 + 70 = 120`
and EMEA is `80`. Prepare it separately in a **new, isolated test database** with
your database client. The suite runner never executes this setup SQL. Do not run
the inserts twice; use a fresh fixture for each preparation. No setup command
below drops or truncates existing data.

## Doris setup

```sql
CREATE DATABASE metis_regression;
CREATE TABLE metis_regression.orders (
  region VARCHAR(16) NOT NULL,
  amount DECIMAL(18, 2) NOT NULL
) DUPLICATE KEY(region)
DISTRIBUTED BY HASH(region) BUCKETS 1
PROPERTIES ("replication_num" = "1");
INSERT INTO metis_regression.orders VALUES ('APAC', 50), ('APAC', 70), ('EMEA', 80);
```

## ClickHouse setup

```sql
CREATE DATABASE metis_regression;
CREATE TABLE metis_regression.orders (
  region String,
  amount Decimal(18, 2)
) ENGINE = MergeTree ORDER BY region;
INSERT INTO metis_regression.orders VALUES ('APAC', 50), ('APAC', 70), ('EMEA', 80);
```

## Run

Provision a read-only account that can SELECT the fixture table. Set
`METIS_REGRESSION_USERNAME` and `METIS_REGRESSION_PASSWORD` via your CI secret
store. For Doris also set `METIS_REGRESSION_DORIS_HOST` and
`METIS_REGRESSION_DORIS_PORT`; for ClickHouse set
`METIS_REGRESSION_CLICKHOUSE_ADDRESS` (`host:port` for its HTTP endpoint).
The example uses a local HTTP ClickHouse endpoint; use the appropriate TLS
configuration for a remote deployment. Never commit secret values.

From the repository root, after building `bin/metis`:

```sh
bin/metis semantic test --mode runtime --project regression \
  --config examples/regression/metis-doris.yaml \
  --suite examples/regression/results.yaml \
  --output ./doris-results.json --junit-output ./doris-results.xml

bin/metis semantic test --mode runtime --project regression \
  --config examples/regression/metis-clickhouse.yaml \
  --suite examples/regression/results.yaml \
  --output ./clickhouse-results.json --junit-output ./clickhouse-results.xml
```

Existing reports are refused; explicitly add `--overwrite` to replace them.
`externally_prepared` is a declaration, not a verified snapshot. Freeze writes
to the fixture while running. The local runner tests semantic answers, not
custom authorization policies. CI must treat any nonzero exit as a failed run.
See the [suite contract](../../docs/specs/testing/project-compile-regression.md)
for comparison semantics, limits, and report privacy.

## CI integration

Use a fresh, private artifact directory per run. The supplied
[`run-ci.sh`](run-ci.sh) preserves the CLI exit code through `tee` using Bash
`pipefail`: a result mismatch exits `1`, and invalid input/report paths exit
`2`. Do not add `|| true`, `continue-on-error`, or a successful upload command
in place of checking that exit status. Compile/schema checks alone cannot
detect changing `SUM` to `AVG`: APAC would become `60` instead of `120`.
The wrapper exclusively creates its private console log and refuses existing
files or symlinks before running the suite, so a log cannot overwrite inputs.

For example, these steps can be added to an existing GitHub Actions job after
building Metis and independently preparing the read-only Doris fixture:

```yaml
- name: Check semantic results
  shell: bash
  env:
    METIS_BIN: ./bin/metis
    METIS_CI_LOG: ./semantic-reports/console.log
    METIS_REGRESSION_USERNAME: ${{ secrets.METIS_REGRESSION_USERNAME }}
    METIS_REGRESSION_PASSWORD: ${{ secrets.METIS_REGRESSION_PASSWORD }}
    METIS_REGRESSION_DORIS_HOST: ${{ vars.METIS_REGRESSION_DORIS_HOST }}
    METIS_REGRESSION_DORIS_PORT: ${{ vars.METIS_REGRESSION_DORIS_PORT }}
  run: |
    set -euo pipefail
    umask 077
    mkdir -m 700 semantic-reports
    bash examples/regression/run-ci.sh \
      --mode runtime --project regression \
      --config examples/regression/metis-doris.yaml \
      --suite examples/regression/results.yaml \
      --output semantic-reports/results.json \
      --junit-output semantic-reports/results.xml
- name: Preserve diagnostics even when the suite fails
  if: ${{ always() }}
  uses: actions/upload-artifact@v4
  with:
    name: semantic-regression
    path: semantic-reports/
    if-no-files-found: warn
```

The upload step does not override the failed check step. Configure repository
artifact access and retention appropriately; console diagnostics may contain
local file paths. JSON/JUnit reports deliberately omit SQL and result values.
Missing configuration produces failed suite status with `not_run` cases and
JUnit errors, not successful skips. Invalid input may produce no report; the
process exit code is authoritative in that case.

The embedded DuckDB CI gate exercises this exact wrapper with a built CLI,
the shared suite above, independent fixture setup, a temporary `SUM` → `AVG`
model mutation, missing configuration, invalid suite, and blocked report path.
Negative cases assert their expected nonzero exits inside the test harness;
they never require a permanently failing workflow. Existing `make
test-duckdb-backend` includes this acceptance test; no new CI service is needed.
