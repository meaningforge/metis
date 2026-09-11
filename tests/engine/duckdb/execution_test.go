//go:build duckdb

package duckdb_test

import (
	"context"
	"testing"

	duckdbbackend "github.com/meaningforge/metis/execution/backend/duckdb"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/engine/duckdb/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

// The backend is named for the dialect it validates, not for the engine running
// it. What is under test is the DuckDB renderer; DuckDB is the engine that
// happens to execute it through the production embedded Driver.
const backendDialect = "DUCKDB"

func TestSharedSemanticScenarios(t *testing.T) {
	backend, err := fixture.New(databasePath(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = backend.Close(ctx) })
	harness.RunSharedExecutionContract(t, harness.Backend{
		Name: backendDialect,
		Prepare: func(t *testing.T, fixture fixtures.ID) {
			t.Helper()
			if err := backend.PrepareFixture(ctx, fixture); err != nil {
				t.Fatal(err)
			}
		},
		OpenExecution: func(t *testing.T) *harness.ProductionExecution {
			return openProductionExecution(t, backend.Database)
		},
	})
}

func TestExecutionBackendResilience(t *testing.T) {
	backend, err := fixture.New(databasePath(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := backend.Execute(ctx, "CREATE SCHEMA analytics"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close(ctx) })
	harness.RunBackendResilienceContract(t, harness.ResilienceContract{
		Name:             "duckdb",
		OpenExecution:    func(t *testing.T) *harness.ProductionExecution { return openProductionExecution(t, backend.Database) },
		LongRunningQuery: "SELECT CAST(SUM(i) AS BIGINT) FROM range(1000000000) values(i)",
	})
}

func openProductionExecution(t *testing.T, database string) *harness.ProductionExecution {
	t.Helper()
	return harness.NewProductionExecution(t, datasource.DataSource{
		Type:   "duckdb",
		Config: map[string]string{"path": database},
		Policy: harness.ProductionExecutionPolicy(),
	}, duckdbbackend.New(), nil)
}
