//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

type executor struct{ db *sql.DB }

func (e *executor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	if e == nil || e.db == nil || compiled == nil {
		return nil, fmt.Errorf("DuckDB Executor is not initialized")
	}
	query := compiled.SqlStatement
	arguments := make([]any, len(query.Parameters))
	for index, parameter := range query.Parameters {
		arguments[index] = parameter.Value
		if number, ok := parameter.Value.(json.Number); ok {
			arguments[index] = number.String()
		}
	}
	// Preparing explicitly lets database/sql enforce the driver-reported
	// parameter count, including excess arguments.
	prepared, err := e.db.PrepareContext(ctx, query.SQL)
	if err != nil {
		return nil, err
	}
	defer prepared.Close()
	rows, err := prepared.QueryContext(ctx, arguments...)
	if err != nil {
		return nil, err
	}
	return &resultStream{rows: rows}, nil
}

func (*executor) Close() error { return nil }
