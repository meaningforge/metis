package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

type executor struct{ db *sql.DB }

func (e *executor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	if e == nil || e.db == nil || compiled == nil {
		return nil, fmt.Errorf("ClickHouse Executor is not initialized")
	}
	query := compiled.SqlRenderResult
	text, parameters, err := bindServerParameters(query)
	if err != nil {
		return nil, err
	}
	if len(parameters) > 0 {
		ctx = clickhousedriver.Context(ctx, clickhousedriver.WithParameters(parameters))
	}
	rows, err := e.db.QueryContext(ctx, text)
	if err != nil {
		return nil, err
	}
	return &resultStream{rows: rows}, nil
}

func (*executor) Close() error { return nil }
