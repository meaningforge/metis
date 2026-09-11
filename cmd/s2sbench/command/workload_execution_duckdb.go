//go:build duckdb

package command

import (
	"context"
	"os"

	duckdbfixture "github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/renderer/sql"
)

type bundleDuckDBExecution struct {
	backend *duckdbfixture.Backend
	schema  string
}

func newBundleDuckDBExecution(root string, bundle workload.Bundle) (*bundleDuckDBExecution, func() error, error) {
	database, err := workload.ResolveAsset(root, bundle.Database.Path)
	if err != nil {
		return nil, nil, err
	}
	schemaPath, err := workload.ResolveAsset(root, bundle.Schema.Path)
	if err != nil {
		return nil, nil, err
	}
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, nil, err
	}
	backend, err := duckdbfixture.New(database)
	if err != nil {
		return nil, nil, err
	}
	execution := &bundleDuckDBExecution{backend: backend, schema: string(schema)}
	return execution, func() error { return backend.Close(context.Background()) }, nil
}

func (*bundleDuckDBExecution) Prepare(context.Context, scenarios.Scenario) error { return nil }

func (e *bundleDuckDBExecution) PhysicalSchema(context.Context) (string, error) { return e.schema, nil }

func (e *bundleDuckDBExecution) RunSQL(ctx context.Context, sql string, params ...sql.QueryParameter) (scenarios.ResultSet, error) {
	return e.backend.RunSQL(ctx, sql, params...)
}
