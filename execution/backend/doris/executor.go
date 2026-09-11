package doris

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

type executor struct{ db *sql.DB }

func (e *executor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	if e == nil || e.db == nil || compiled == nil {
		return nil, fmt.Errorf("Doris Executor is not initialized")
	}
	query := compiled.SqlStatement
	arguments := make([]any, len(query.Parameters))
	for index, parameter := range query.Parameters {
		arguments[index] = parameter.Value
	}
	rows, err := e.db.QueryContext(ctx, query.SQL, arguments...)
	if err != nil {
		return nil, err
	}
	return &resultStream{rows: rows}, nil
}

func (*executor) Close() error { return nil }
