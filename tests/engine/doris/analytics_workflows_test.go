package doris_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/execution/backend"
	dorisbackend "github.com/meaningforge/metis/execution/backend/doris"
	"github.com/meaningforge/metis/execution/datasource"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestAnalyticsWorkflowsThroughProductionDorisBackend(t *testing.T) {
	dsn := dorisDSN(t)
	connection, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse test Doris DataSource: %v", err)
	}
	if connection.Net != "tcp" || strings.TrimSpace(connection.Addr) == "" {
		t.Fatalf("test Doris DataSource must identify a TCP address, got net=%q address=%q", connection.Net, connection.Addr)
	}
	host, port, err := net.SplitHostPort(connection.Addr)
	if err != nil {
		t.Fatalf("split test Doris address %q: %v", connection.Addr, err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("connect to Doris: %v", err)
	}
	waitForBackend(t, db)
	prepareFixture(t, db, enginefixture.AnalyticsWorkflows)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeAnalyticsWorkflowFile(t, filepath.Join(dir, "analytics.ossie.yaml"), enginefixture.AnalyticsModelYAML)
	writeAnalyticsWorkflowFile(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  analytics:\n    path: ./analytics.ossie.yaml\n")
	datasourceConfig := fmt.Sprintf("doris-local:\n  type: doris\n  config:\n    host: %s\n    port: %s\n    username: %s\n", strconv.Quote(host), strconv.Quote(port), strconv.Quote(connection.User))
	var secrets bootstrap.RuntimeOption
	if connection.Passwd != "" {
		datasourceConfig += "    password: \"${METIS_TEST_DORIS_PASSWORD}\"\n"
		secrets = bootstrap.WithSecretResolver(staticSecretResolver(connection.Passwd))
	}
	datasourceConfig += "  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1048576\n    max_concurrency: 4\n"
	writeAnalyticsWorkflowFile(t, filepath.Join(dir, "datasources.yaml"), datasourceConfig)
	configPath := filepath.Join(dir, "metis.yaml")
	writeAnalyticsWorkflowFile(t, configPath, "projects:\n  analytics:\n    path: ./project.yaml\n    data_source: doris-local\ndata_sources:\n  path: ./datasources.yaml\n")

	backends, err := backend.NewBackendRegistry(dorisbackend.New())
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
			t.Errorf("close Doris runtime: %v", err)
		}
	})

	harness.RunQueryMetricsContract(t, harness.QueryMetricsRuntime{
		Name:         "DORIS",
		QueryMetrics: runtime.QueryMetrics.QueryMetrics,
	})
	harness.RunAnalyticsWorkflowContract(t, harness.AnalyticsWorkflowRuntime{
		Name:            "DORIS",
		AttributeMetric: runtime.AttributeMetric.AttributeMetric,
		CompareMetrics:  runtime.CompareMetrics.CompareMetrics,
	})
}

type staticSecretResolver string

func (r staticSecretResolver) ResolveSecret(context.Context, datasource.SecretRef) (string, error) {
	return string(r), nil
}

func writeAnalyticsWorkflowFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
