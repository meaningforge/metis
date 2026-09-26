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
bin/metis project test --mode runtime --project regression \
  --config examples/regression/metis-doris.yaml \
  --suite examples/regression/results.yaml \
  --output ./doris-results.json --junit-output ./doris-results.xml

bin/metis project test --mode runtime --project regression \
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
