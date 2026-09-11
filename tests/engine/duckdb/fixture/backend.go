//go:build duckdb

// Package fixture owns embedded DuckDB fixture setup plus S2SBench's
// deliberately arbitrary-SQL execution. Real-engine conformance uses the
// shared production Runner harness; direct writable connections are used only
// to seed isolated test databases and inspect S2SBench query schemas.
package fixture

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend/duckdb"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

type Backend struct {
	Database string
	mu       sync.Mutex
	runtime  driver.Runtime
}

func New(database string) (*Backend, error) {
	database = strings.TrimSpace(database)
	if database == "" {
		return nil, fmt.Errorf("DuckDB database path is required")
	}
	return &Backend{Database: database}, nil
}

func (b *Backend) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeRuntime(ctx)
}

func (b *Backend) RunSQL(ctx context.Context, query string, params ...sql.QueryParameter) (scenarios.ResultSet, error) {
	if b == nil {
		return scenarios.ResultSet{}, fmt.Errorf("DuckDB fixture is required")
	}
	statement := strings.TrimSuffix(strings.TrimSpace(query), ";")
	if statement == "" {
		return scenarios.ResultSet{}, fmt.Errorf("DuckDB SQL is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	columns, err := b.describe(ctx, statement, params...)
	if err != nil {
		return scenarios.ResultSet{}, err
	}
	runtime, err := b.productionRuntime(ctx)
	if err != nil {
		return scenarios.ResultSet{}, err
	}
	executor, err := runtime.Acquire(ctx)
	if err != nil {
		return scenarios.ResultSet{}, err
	}
	defer executor.Close()
	stream, err := executor.Execute(ctx, &artifact.CompiledQuery{SQLRenderResult: sql.SQLRenderResult{Dialect: "DUCKDB", SQL: statement, Parameters: params}})
	if err != nil {
		return scenarios.ResultSet{}, fmt.Errorf("execute DuckDB SQL through production Driver: %w", err)
	}
	defer stream.Close()
	var rows []scenarios.ResultRow
	for {
		values, nextErr := stream.Next(ctx)
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return scenarios.ResultSet{}, fmt.Errorf("read DuckDB production result: %w", nextErr)
		}
		if len(values) != len(columns) {
			return scenarios.ResultSet{}, fmt.Errorf("DuckDB returned %d values for %d columns", len(values), len(columns))
		}
		row := make(scenarios.ResultRow, len(values))
		for i, value := range values {
			normalized, normalizeErr := normalizeValue(value, columns[i].ValueKind)
			if normalizeErr != nil {
				return scenarios.ResultSet{}, fmt.Errorf("column %q: %w", columns[i].Name, normalizeErr)
			}
			row[i] = normalized
		}
		rows = append(rows, row)
	}
	return scenarios.ResultSet{Columns: columns, Rows: rows}, nil
}

func (b *Backend) PhysicalSchema(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	db, err := b.openDirect(ctx, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT sql FROM duckdb_tables() WHERE schema_name = 'analytics' ORDER BY table_name")
	if err != nil {
		return "", fmt.Errorf("inspect DuckDB physical schema: %w", err)
	}
	defer rows.Close()
	var out strings.Builder
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			return "", err
		}
		out.WriteString(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
		out.WriteString(";\n")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("DuckDB analytics schema has no tables")
	}
	return out.String(), nil
}

func (b *Backend) Execute(ctx context.Context, statements ...string) error {
	if b == nil {
		return fmt.Errorf("DuckDB fixture is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.closeRuntime(ctx); err != nil {
		return err
	}
	db, err := b.openDirect(ctx, false)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("seed DuckDB fixture: %w", err)
		}
	}
	return nil
}

func (b *Backend) execute(ctx context.Context, statements ...string) error {
	return b.Execute(ctx, statements...)
}

func (b *Backend) productionRuntime(ctx context.Context) (driver.Runtime, error) {
	if b.runtime != nil {
		return b.runtime, nil
	}
	runtime, err := duckdb.NewDriverFactory().OpenDataSource(ctx, driver.OpenRequest{Config: map[string]string{"path": b.Database}})
	if err != nil {
		return nil, fmt.Errorf("open production DuckDB Driver: %w", err)
	}
	b.runtime = runtime
	return runtime, nil
}

func (b *Backend) closeRuntime(ctx context.Context) error {
	if b.runtime == nil {
		return nil
	}
	err := b.runtime.Close(ctx)
	b.runtime = nil
	return err
}

func (b *Backend) describe(ctx context.Context, statement string, params ...sql.QueryParameter) ([]scenarios.ResultColumn, error) {
	db, err := b.openDirect(ctx, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	arguments := make([]any, len(params))
	for i, parameter := range params {
		arguments[i] = parameter.Value
		if number, ok := parameter.Value.(json.Number); ok {
			arguments[i] = number.String()
		}
	}
	prepared, err := db.PrepareContext(ctx, "DESCRIBE ("+statement+")")
	if err != nil {
		return nil, fmt.Errorf("prepare DuckDB description: %w", err)
	}
	defer prepared.Close()
	rows, err := prepared.QueryContext(ctx, arguments...)
	if err != nil {
		return nil, fmt.Errorf("describe DuckDB SQL: %w", err)
	}
	defer rows.Close()
	var columns []scenarios.ResultColumn
	for rows.Next() {
		var name, physicalType string
		var nullable, key, defaultValue, extra any
		if err := rows.Scan(&name, &physicalType, &nullable, &key, &defaultValue, &extra); err != nil {
			return nil, err
		}
		kind, err := duckDBResultKind(physicalType)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", name, err)
		}
		columns = append(columns, scenarios.ResultColumn{Name: name, ValueKind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("DESCRIBE returned no column metadata")
	}
	return columns, nil
}
