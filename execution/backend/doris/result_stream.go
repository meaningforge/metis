package doris

import (
	"context"
	"database/sql"
	"fmt"
	"io"
)

type resultStream struct{ rows *sql.Rows }

func (s *resultStream) Next(ctx context.Context) ([]any, error) {
	if s == nil || s.rows == nil {
		return nil, fmt.Errorf("Doris result stream is not initialized")
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
	return values, nil
}

func (s *resultStream) Close() error {
	if s == nil || s.rows == nil {
		return nil
	}
	return s.rows.Close()
}
