package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
)

// Explain discards bounded planning output. The pinned Doris version does not
// accept the bound EXPLAIN path and ClickHouse's positional binder interpolates.
// Parameterized shapes therefore remain unsupported without any query I/O.
func Explain(ctx context.Context, db *sql.DB, compiled *artifact.CompiledQuery, limits driver.CatalogLimits, backend string) (evidence driver.ValidationEvidence, err error) {
	evidence = driver.ValidationEvidence{Outcome: "unsupported", Method: "explain"}
	if compiled == nil || db == nil || limits.MaxColumns <= 0 || limits.MaxBytes <= 0 {
		return evidence, fmt.Errorf("invalid validation operation")
	}
	if backend != "doris" && backend != "clickhouse" {
		return evidence, nil
	}
	if len(compiled.SqlRenderResult.Parameters) > 0 {
		return evidence, nil
	}
	statement := strings.TrimSpace(compiled.SqlRenderResult.SQL)
	upper := strings.ToUpper(statement)
	if !(strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "SELECT\n") || strings.HasPrefix(upper, "WITH ") || strings.HasPrefix(upper, "WITH\n")) {
		return evidence, nil
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN "+statement)
	if err != nil {
		return driver.ValidationEvidence{}, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 || len(columns) > limits.MaxColumns {
		return driver.ValidationEvidence{}, fmt.Errorf("invalid planning evidence")
	}
	values := make([]sql.RawBytes, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	count, size := 0, 0
	for rows.Next() {
		count++
		if count > limits.MaxColumns {
			return driver.ValidationEvidence{}, fmt.Errorf("planning evidence exceeds limit")
		}
		if err := rows.Scan(targets...); err != nil {
			return driver.ValidationEvidence{}, err
		}
		for _, value := range values {
			size += len(value)
		}
		if size > limits.MaxBytes {
			return driver.ValidationEvidence{}, fmt.Errorf("planning evidence exceeds limit")
		}
	}
	if err := rows.Err(); err != nil {
		return driver.ValidationEvidence{}, err
	}
	if count == 0 {
		return driver.ValidationEvidence{}, fmt.Errorf("empty planning evidence")
	}
	return driver.ValidationEvidence{Outcome: "accepted", Method: "explain"}, nil
}
