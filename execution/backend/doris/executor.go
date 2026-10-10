package doris

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
	rendersql "github.com/meaningforge/metis/renderer/sql"
)

type executor struct{ db *sql.DB }

func (e *executor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	if e == nil || e.db == nil || compiled == nil {
		return nil, fmt.Errorf("Doris Executor is not initialized")
	}
	query := compiled.SqlRenderResult
	arguments := dorisArguments(query.Parameters)
	rows, err := e.db.QueryContext(ctx, query.SQL, arguments...)
	if err != nil {
		return nil, err
	}
	return &resultStream{rows: rows}, nil
}

func dorisArguments(parameters []rendersql.QueryParameter) []any {
	arguments := make([]any, len(parameters))
	for index, parameter := range parameters {
		arguments[index] = parameter.Value
		if number, ok := parameter.Value.(json.Number); ok {
			arguments[index] = number.String()
		}
	}
	return arguments
}

func (*executor) Close() error { return nil }
