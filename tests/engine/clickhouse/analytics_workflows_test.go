package clickhouse_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/execution/backend"
	clickhousebackend "github.com/meaningforge/metis/execution/backend/clickhouse"
	"github.com/meaningforge/metis/execution/datasource"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestQueryMetricsThroughProductionClickHouseBackend(t *testing.T) {
	endpoint, err := url.Parse(clickHouseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	prepareFixture(t, endpoint.String(), enginefixture.AnalyticsWorkflows)

	dir := t.TempDir()
	writeClickHouseWorkflowFile(t, filepath.Join(dir, "analytics.ossie.yaml"), enginefixture.AnalyticsModelYAML)
	writeClickHouseWorkflowFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  analytics:\n    path: ./analytics.ossie.yaml\n")
	username := ""
	password := ""
	if endpoint.User != nil {
		username = endpoint.User.Username()
		password, _ = endpoint.User.Password()
	}
	database := endpoint.Query().Get("database")
	datasourceConfig := fmt.Sprintf("clickhouse-local:\n  type: clickhouse\n  config:\n    scheme: %q\n    address: %q\n    database: %q\n    username: %q\n", endpoint.Scheme, endpoint.Host, database, username)
	var secrets bootstrap.RuntimeOption
	if password != "" {
		datasourceConfig += "    password: \"${METIS_TEST_CLICKHOUSE_PASSWORD}\"\n"
		secrets = bootstrap.WithSecretResolver(clickHouseStaticSecretResolver(password))
	}
	datasourceConfig += "  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1048576\n    max_concurrency: 4\n"
	writeClickHouseWorkflowFile(t, filepath.Join(dir, "datasources.yaml"), datasourceConfig)
	configPath := filepath.Join(dir, "metis.yaml")
	writeClickHouseWorkflowFile(t, configPath, "projects:\n  analytics:\n    path: ./project.yaml\n    data_source: clickhouse-local\ndata_sources:\n  path: ./datasources.yaml\n")

	backends, err := backend.NewBackendRegistry(clickhousebackend.New())
	if err != nil {
		t.Fatal(err)
	}
	options := []bootstrap.RuntimeOption{bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization()}
	if secrets != nil {
		options = append(options, secrets)
	}
	runtime, err := bootstrap.LoadRuntime(configPath, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Execution.Close(context.Background()); err != nil {
			t.Errorf("close ClickHouse runtime: %v", err)
		}
	})

	harness.RunQueryMetricsContract(t, harness.QueryMetricsRuntime{
		Name:         "CLICKHOUSE",
		QueryMetrics: runtime.QueryMetrics.QueryMetrics,
	})
}

type clickHouseStaticSecretResolver string

func (r clickHouseStaticSecretResolver) ResolveSecret(context.Context, datasource.SecretRef) (string, error) {
	return string(r), nil
}

func writeClickHouseWorkflowFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
