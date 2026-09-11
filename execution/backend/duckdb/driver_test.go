//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/ossie"
	sqlquery "github.com/meaningforge/metis/renderer/sql"
)

func TestNewBindsOneExecutableDuckDBBackend(t *testing.T) {
	binding := New()
	if binding.Type != "duckdb" || binding.Renderer == nil || binding.DriverFactory == nil || binding.SQLDialect() != "DUCKDB" {
		t.Fatalf("DuckDB Backend = %#v", binding)
	}
	if _, err := backend.NewBackendRegistry(binding); err != nil {
		t.Fatalf("NewBackendRegistry(DuckDB) error = %v", err)
	}
}

func TestDuckDBDriverFactoryValidatesClosedConnectionConfig(t *testing.T) {
	factory := NewDriverFactory()
	if err := factory.ValidateConfig(map[string]string{"path": "/var/lib/metis/analytics.duckdb"}); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, config := range map[string]map[string]string{
		"missing path":      {},
		"blank path":        {"path": " "},
		"in-memory path":    {"path": ":memory:"},
		"untrimmed path":    {"path": " analytics.duckdb "},
		"option injection":  {"path": "analytics.duckdb?access_mode=read_write"},
		"option hiding":     {"path": "analytics#fragment.duckdb"},
		"invalid reference": {"path": "${duckdb-path}"},
		"unknown field":     {"path": "analytics.duckdb", "threads": "4"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := factory.ValidateConfig(config); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
}

func TestDuckDBRuntimeExecutesReadOnlyQueriesAndPreservesDecimalText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metis.duckdb")
	seedDuckDB(t, path)

	runtime, err := NewDriverFactory().OpenDataSource(context.Background(), driver.OpenRequest{Config: map[string]string{"path": path}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := runtime.Close(context.Background()); closeErr != nil {
			t.Errorf("close runtime: %v", closeErr)
		}
	})
	executor, err := runtime.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := executor.Execute(context.Background(), compiledDuckDBQuery("SELECT SUM(amount) FROM analytics.orders"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	row, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(row) != 1 || row[0] != "12.5" {
		t.Fatalf("DuckDB row = %#v, want decimal text", row)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("second Next error = %v, want EOF", err)
	}
}

func TestDuckDBDriverFactoryFailsClosedOnMissingFileAndCancellation(t *testing.T) {
	factory := NewDriverFactory()
	missing := filepath.Join(t.TempDir(), "missing.duckdb")
	if _, err := factory.OpenDataSource(context.Background(), driver.OpenRequest{Config: map[string]string{"path": missing}}); err == nil {
		t.Fatal("read-only DuckDB factory created a missing database")
	}
	path := filepath.Join(t.TempDir(), "metis.duckdb")
	seedDuckDB(t, path)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.OpenDataSource(cancelled, driver.OpenRequest{Config: map[string]string{"path": path}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled DuckDB open error = %v, want context.Canceled", err)
	}
}

func TestDuckDBRuntimeRejectsWritesAndHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metis.duckdb")
	seedDuckDB(t, path)
	runtime, err := NewDriverFactory().OpenDataSource(context.Background(), driver.OpenRequest{Config: map[string]string{"path": path}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	executor, err := runtime.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), compiledDuckDBQuery("CREATE TABLE forbidden(value INTEGER)")); err == nil {
		t.Fatal("read-only DuckDB Backend accepted a write")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Execute(cancelled, compiledDuckDBQuery("SELECT amount FROM analytics.orders")); err == nil {
		t.Fatal("DuckDB execution ignored a cancelled context")
	}
}

func TestDuckDBRuntimeSharesDatabaseAndStopsAcquisitionAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metis.duckdb")
	seedDuckDB(t, path)
	runtime, err := NewDriverFactory().OpenDataSource(context.Background(), driver.OpenRequest{Config: map[string]string{"path": path}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtime.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Acquire(context.Background())
	if err != nil {
		t.Fatalf("closing a request lease closed the shared database: %v", err)
	}
	stream, err := second.Execute(context.Background(), compiledDuckDBQuery("SELECT SUM(amount) FROM analytics.orders"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Acquire(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Acquire(context.Background()); err == nil {
		t.Fatal("closed DuckDB Runtime lent a new Executor")
	}
}

func seedDuckDB(t *testing.T, path string) {
	t.Helper()
	connector, err := duckdb.NewConnector(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	if _, err := db.Exec(`CREATE SCHEMA analytics; CREATE TABLE analytics.orders(amount DECIMAL(10,2)); INSERT INTO analytics.orders VALUES (5.25), (7.25)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func compiledDuckDBQuery(sql string) *artifact.CompiledQuery {
	return &artifact.CompiledQuery{
		SqlRenderResult: sqlquery.SqlRenderResult{Dialect: "DUCKDB", SQL: sql},
		OutputSchema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{
			Name: "total_revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal,
		}}},
	}
}
