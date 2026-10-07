//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	duckdbnative "github.com/duckdb/duckdb-go/v2"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
	renderedsql "github.com/meaningforge/metis/renderer/sql"
)

func TestCatalogAndParameterizedExplainAreReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspection.duckdb")
	connector, err := duckdbnative.NewConnector(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	if _, err := db.Exec(`CREATE SCHEMA analytics; CREATE TABLE analytics.orders(Region VARCHAR NOT NULL, amount DECIMAL(18,2), recorded TIMESTAMP_NS, nested INTEGER[]); CREATE SEQUENCE analytics.probe_sequence; INSERT INTO analytics.orders VALUES ('private-row', 10.25, NULL, NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewDriverFactory().OpenDataSource(context.Background(), driver.OpenRequest{Config: map[string]string{"path": path}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	lease, err := runtime.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	inspector := lease.(driver.CatalogInspector)
	limits := driver.CatalogLimits{MaxColumns: 100, MaxBytes: 10000}
	ref := driver.CatalogReference{ID: "orders", Parts: []string{"analytics", "orders"}}
	evidence, err := inspector.DescribeRelation(context.Background(), ref, limits)
	if err != nil || !evidence.ColumnsComplete || len(evidence.Columns) != 4 {
		t.Fatalf("metadata=%+v err=%v", evidence, err)
	}
	if evidence.Columns[0].Name != "Region" || evidence.Columns[0].Nullable != "not_null" || evidence.Columns[1].NativeType.Scale == nil || *evidence.Columns[1].NativeType.Scale != 2 || evidence.Columns[2].NativeType.Precision == nil || *evidence.Columns[2].NativeType.Precision != 9 || evidence.Columns[3].NativeType.Name != "INTEGER[]" {
		t.Fatalf("lost native evidence: %+v", evidence.Columns)
	}
	if _, err := inspector.DescribeRelation(context.Background(), ref, driver.CatalogLimits{MaxColumns: 1, MaxBytes: 10000}); err == nil {
		t.Fatal("ignored metadata budget")
	}
	qualified := driver.CatalogReference{ID: "orders", Parts: []string{"inspection", "analytics", "orders"}}
	if got, err := inspector.DescribeRelation(context.Background(), qualified, limits); err != nil || len(got.Columns) != 4 {
		t.Fatalf("three-part relation=%+v err=%v", got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspector.DescribeRelation(cancelled, ref, limits); err == nil {
		t.Fatal("ignored cancellation")
	}
	ref.Parts[1] = "absent"
	if _, err := inspector.DescribeRelation(context.Background(), ref, limits); err == nil {
		t.Fatal("invented absent table evidence")
	}
	validator := lease.(driver.CompiledQueryValidator)
	for _, text := range []string{"SELECT SUM(amount) FROM analytics.orders WHERE Region = ?"} {
		compiled := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{Dialect: "DUCKDB", SQL: text, Parameters: []renderedsql.QueryParameter{{Value: "private-row"}}}}
		result, err := validator.ValidateCompiled(context.Background(), compiled, limits)
		if err != nil || result.Outcome != "accepted" {
			t.Fatalf("parameterized planning: %+v err=%v", result, err)
		}
	}
	for _, text := range []string{"SELECT absent_column FROM analytics.orders WHERE Region = ?", "SELECT * FROM analytics.absent WHERE x = ?"} {
		compiled := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{SQL: text, Parameters: []renderedsql.QueryParameter{{Value: "private"}}}}
		if result, err := validator.ValidateCompiled(context.Background(), compiled, limits); err == nil && result.Outcome == "accepted" {
			t.Fatal("accepted invalid planning")
		}
	}
	compiledCount := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{SQL: "SELECT ?", Parameters: []renderedsql.QueryParameter{{Value: 1}, {Value: 2}}}}
	if result, err := validator.ValidateCompiled(context.Background(), compiledCount, limits); err == nil && result.Outcome == "accepted" {
		t.Fatal("ignored excess parameters")
	}
	lease.Close()
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	connector, err = duckdbnative.NewConnector(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	db = sql.OpenDB(connector)
	defer db.Close()
	// A writable test-only connection lets the counter distinguish planning
	// from execution; production connections remain read-only throughout.
	probe := &executor{db: db}
	compiled := &artifact.CompiledQuery{SqlRenderResult: renderedsql.SqlRenderResult{Dialect: "DUCKDB", SQL: "SELECT nextval('analytics.probe_sequence') + ?", Parameters: []renderedsql.QueryParameter{{Value: int64(2)}}}}
	if result, err := probe.ValidateCompiled(context.Background(), compiled, limits); err != nil || result.Outcome != "accepted" {
		t.Fatalf("non-executing probe=%+v err=%v", result, err)
	}
	var value int64
	if err := db.QueryRow("SELECT nextval('analytics.probe_sequence')").Scan(&value); err != nil || value != 1 {
		t.Fatalf("EXPLAIN executed query: sequence=%d err=%v", value, err)
	}
}
