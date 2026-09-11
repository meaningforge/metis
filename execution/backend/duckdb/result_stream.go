//go:build duckdb

package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math/big"

	"github.com/duckdb/duckdb-go/v2"
)

type resultStream struct{ rows *sql.Rows }

func (s *resultStream) Next(ctx context.Context) ([]any, error) {
	if s == nil || s.rows == nil {
		return nil, fmt.Errorf("DuckDB result stream is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.rows.Next() {
		if err := s.rows.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	columns, err := s.rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(columns))
	destinations := make([]any, len(columns))
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := s.rows.Scan(destinations...); err != nil {
		return nil, err
	}
	for index, value := range values {
		switch typed := value.(type) {
		case duckdb.Decimal:
			values[index] = typed.String()
		case *big.Int:
			values[index] = typed.String()
		}
	}
	return values, nil
}

func (s *resultStream) Close() error {
	if s == nil || s.rows == nil {
		return nil
	}
	return s.rows.Close()
}
