package datasource

import (
	"errors"
	"strings"
	"testing"

	runtimedatasource "github.com/meaningforge/metis/execution/datasource"
)

func TestResolveCheckedInDataSourceDefinitionsFromEnvironment(t *testing.T) {
	for name, key := range map[string]string{"clickhouse": "address", "doris": "host"} {
		t.Run(name, func(t *testing.T) {
			dataSource, err := Resolve(name, func(variable string) (string, bool) { return "resolved-" + variable, true })
			if err != nil {
				t.Fatal(err)
			}
			value, ok := dataSource.Value(key)
			if !ok || !strings.HasPrefix(value, "resolved-") {
				t.Fatalf("resolved value = %q, present=%t", value, ok)
			}
			if dataSource.Type() != name {
				t.Fatalf("DataSource type = %q, want %q", dataSource.Type(), name)
			}
			if name == "clickhouse" {
				if configured, ok := dataSource.Config("password"); !ok || configured != "${METIS_TEST_CLICKHOUSE_PASSWORD}" {
					t.Fatalf("password Config = %q, present=%t", configured, ok)
				}
				if secret, ok := dataSource.Secret("password"); !ok || secret != "resolved-METIS_TEST_CLICKHOUSE_PASSWORD" {
					t.Fatalf("resolved password secret present=%t", ok)
				}
			}
		})
	}
}

func TestRuntimeDataSourcePreservesReferencesWithoutResolvedValues(t *testing.T) {
	dataSource, err := Resolve("clickhouse", func(variable string) (string, bool) { return "resolved-" + variable, true })
	if err != nil {
		t.Fatal(err)
	}
	runtime := dataSource.RuntimeDataSource(runtimedatasource.DataSourcePolicy{QueryTimeout: "1s"})
	if runtime.Type != "clickhouse" || runtime.Config["address"] != "${METIS_TEST_CLICKHOUSE_ADDRESS}" || runtime.Config["password"] != "${METIS_TEST_CLICKHOUSE_PASSWORD}" {
		t.Fatalf("runtime DataSource did not preserve configured references: %#v", runtime)
	}
	for _, value := range runtime.Config {
		if strings.HasPrefix(value, "resolved-") {
			t.Fatal("resolved test connection value escaped into production DataSource config")
		}
	}
}

func TestResolveRejectsPlaintextSensitiveConfig(t *testing.T) {
	definitions := document{"warehouse": {
		Type:   "warehouse",
		Config: map[string]string{"host": "warehouse.internal", "password": "plaintext"},
	}}
	if _, err := resolve(definitions, "warehouse", func(string) (string, bool) { return "", false }); err == nil || !strings.Contains(err.Error(), "must be an environment reference") {
		t.Fatalf("plaintext sensitive config error = %v", err)
	}
}

func TestResolveAcceptsDataSourceStyleLiteralConfig(t *testing.T) {
	definitions := document{"duckdb": {Type: "duckdb", Config: map[string]string{"path": ":memory:"}}}
	dataSource, err := resolve(definitions, "duckdb", func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if path, ok := dataSource.Value("path"); !ok || path != ":memory:" {
		t.Fatalf("DuckDB path = %q, present=%t", path, ok)
	}
}

func TestResolveReportsMissingEnvironmentWithoutValues(t *testing.T) {
	_, err := Resolve("doris", func(string) (string, bool) { return "", false })
	var missing *MissingEnvironmentError
	if !errors.As(err, &missing) || len(missing.Variables) != 2 || missing.Variables[0] != "METIS_TEST_DORIS_HOST" || missing.Variables[1] != "METIS_TEST_DORIS_PORT" {
		t.Fatalf("missing environment error = %#v, %v", missing, err)
	}
}

func TestResolveSupportsFutureWarehouseShapesWithoutLoaderChanges(t *testing.T) {
	definitions := document{"bigquery": {
		Type: "bigquery",
		Config: map[string]string{
			"project": "${BIGQUERY_PROJECT}",
			"dataset": "${BIGQUERY_DATASET}",
		},
	}}
	dataSource, err := resolve(definitions, "bigquery", func(variable string) (string, bool) { return "value-for-" + variable, true })
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := dataSource.Value("project"); !ok || value != "value-for-BIGQUERY_PROJECT" {
		t.Fatalf("BigQuery project resolution = %q, present=%t", value, ok)
	}
}
