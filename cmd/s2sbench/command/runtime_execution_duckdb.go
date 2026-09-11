//go:build duckdb

package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/execution/backend"
	duckdbbackend "github.com/meaningforge/metis/execution/backend/duckdb"
)

func loadSemanticProjectRuntime(configPath, project, database string, captures ...*physicalQueryCapture) (*bootstrap.Runtime, error) {
	database = strings.TrimSpace(database)
	if database == "" {
		return bootstrap.LoadRuntime(configPath, bootstrap.WithLocalAllAccessProjectAuthorization())
	}
	baseDir := filepath.Dir(configPath)
	datasources := fmt.Sprintf(`%s:
  type: duckdb
  config:
    path: %q
  policy:
    query_timeout: 30s
    max_rows: 1000
    max_bytes: 8388608
    max_concurrency: 1
`, s2sbenchDataSource, database)
	if err := os.WriteFile(filepath.Join(baseDir, "datasources.yaml"), []byte(datasources), 0o600); err != nil {
		return nil, fmt.Errorf("write S2SBench DataSource registry: %w", err)
	}
	deployment := fmt.Sprintf(`version: 1
projects:
  %q:
    path: ./project.yaml
    data_source: %s
data_sources:
  path: ./datasources.yaml
`, project, s2sbenchDataSource)
	if err := os.WriteFile(configPath, []byte(deployment), 0o600); err != nil {
		return nil, fmt.Errorf("write executable S2SBench deployment config: %w", err)
	}
	duckDBBackend := duckdbbackend.New()
	if len(captures) > 0 && captures[0] != nil {
		duckDBBackend.DriverFactory = capturingDriverFactory{inner: duckDBBackend.DriverFactory, sink: captures[0]}
	}
	backends, err := backend.NewBackendRegistry(duckDBBackend)
	if err != nil {
		return nil, fmt.Errorf("compose S2SBench DuckDB Backend: %w", err)
	}
	return bootstrap.LoadRuntime(configPath, bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization())
}
