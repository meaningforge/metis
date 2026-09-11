//go:build duckdb

package fixture

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func duckDBResultKind(physical string) (scenarios.ResultValueKind, error) {
	normalized := strings.ToUpper(strings.TrimSpace(physical))
	if index := strings.Index(normalized, "("); index >= 0 {
		normalized = normalized[:index]
	}
	switch normalized {
	case "VARCHAR", "CHAR", "BPCHAR", "TEXT", "STRING", "UUID", "BLOB":
		return scenarios.ResultString, nil
	case "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "HUGEINT",
		"UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT", "UHUGEINT":
		return scenarios.ResultInteger, nil
	case "DECIMAL", "NUMERIC", "REAL", "FLOAT", "DOUBLE":
		return scenarios.ResultNumber, nil
	case "BOOLEAN", "BOOL":
		return scenarios.ResultBoolean, nil
	case "DATE":
		return scenarios.ResultDate, nil
	case "TIME":
		return scenarios.ResultTime, nil
	case "TIMESTAMP", "TIMESTAMP_S", "TIMESTAMP_MS", "TIMESTAMP_NS", "TIMESTAMP WITH TIME ZONE", "TIMESTAMPTZ":
		return scenarios.ResultDateTime, nil
	}
	return "", fmt.Errorf("unmapped DuckDB type %q", physical)
}

func normalizeValue(raw any, kind scenarios.ResultValueKind) (scenarios.ResultValue, error) {
	if raw == nil {
		return scenarios.NullResultValue(kind), nil
	}
	var literal string
	switch value := raw.(type) {
	case string:
		literal = value
	case []byte:
		literal = string(value)
	case time.Time:
		switch kind {
		case scenarios.ResultDate:
			literal = value.Format("2006-01-02")
		case scenarios.ResultTime:
			literal = value.Format("15:04:05.999999999")
		default:
			literal = value.Format("2006-01-02 15:04:05.999999999-07:00")
		}
	case float32:
		literal = strconv.FormatFloat(float64(value), 'g', -1, 32)
	case float64:
		literal = strconv.FormatFloat(value, 'g', -1, 64)
	default:
		literal = fmt.Sprint(value)
	}
	return scenarios.ParseResultValue(kind, literal)
}
