// Package catalog provides bounded decoding shared by metadata-only warehouse
// adapters. It does not own connections, secrets, routing, or semantic models.
package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/meaningforge/metis/execution/driver"
)

type TypeParser func(string) (driver.CatalogNativeType, string, error)

// Describe discards defaults, comments and other non-column evidence. Unexpected
// wire layouts, empty inventories and limits fail without a partial result.
func Describe(ctx context.Context, db *sql.DB, backend string, ref driver.CatalogReference, limits driver.CatalogLimits, parse TypeParser) (result driver.CatalogRelation, err error) {
	if err := driver.ValidateCatalogReference(backend, ref); err != nil {
		return result, err
	}
	if limits.MaxColumns <= 0 || limits.MaxColumns > driver.MaxCatalogColumns || limits.MaxBytes <= 0 || limits.MaxBytes > driver.MaxCatalogBytes {
		return result, fmt.Errorf("invalid metadata limits")
	}
	quoted := make([]string, len(ref.Parts))
	for i, part := range ref.Parts {
		quoted[i] = "`" + part + "`"
	}
	statement := "DESCRIBE " + strings.Join(quoted, ".")
	nameField, typeField, nullField := "Field", "Type", "Null"
	if backend == "clickhouse" {
		statement = "DESCRIBE TABLE " + strings.Join(quoted, ".") + " SETTINGS describe_include_subcolumns = 0"
		nameField, typeField, nullField = "name", "type", ""
	}
	if backend == "duckdb" {
		for i, part := range ref.Parts {
			quoted[i] = "\"" + part + "\""
		}
		statement = "DESCRIBE " + strings.Join(quoted, ".")
		nameField, typeField, nullField = "column_name", "column_type", "null"
	}
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			result = driver.CatalogRelation{}
			err = closeErr
		}
	}()
	fields, err := rows.Columns()
	if err != nil {
		return result, err
	}
	if len(fields) > 64 {
		return result, fmt.Errorf("unexpected metadata layout")
	}
	positions := map[string]int{}
	for i, field := range fields {
		if _, exists := positions[field]; exists {
			return result, fmt.Errorf("duplicate metadata field")
		}
		positions[field] = i
	}
	for _, required := range []string{nameField, typeField, nullField} {
		if required != "" {
			if _, exists := positions[required]; !exists {
				return result, fmt.Errorf("missing metadata field")
			}
		}
	}
	columns := []driver.CatalogColumn{}
	seen := map[string]bool{}
	bytesUsed := 0
	for rows.Next() {
		if len(columns) >= limits.MaxColumns {
			return result, fmt.Errorf("metadata column limit")
		}
		values, destinations := make([]any, len(fields)), make([]any, len(fields))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return result, err
		}
		// Account for discarded fields too; oversized descriptions cannot bypass
		// the inspection ceiling through comments or default expressions.
		for _, value := range values {
			switch v := value.(type) {
			case string:
				bytesUsed += len(v)
			case []byte:
				bytesUsed += len(v)
			case nil:
			default:
				return result, fmt.Errorf("unexpected metadata value")
			}
		}
		if bytesUsed > limits.MaxBytes {
			return result, fmt.Errorf("metadata byte limit")
		}
		name, ok := text(values[positions[nameField]])
		if !ok || name == "" || seen[name] {
			return result, fmt.Errorf("invalid column identity")
		}
		seen[name] = true
		typeText, ok := text(values[positions[typeField]])
		if !ok {
			return result, fmt.Errorf("invalid type evidence")
		}
		native, nullable, err := parse(typeText)
		if err != nil {
			return result, err
		}
		if nullField != "" {
			nullText, ok := text(values[positions[nullField]])
			if !ok {
				return result, fmt.Errorf("invalid nullability")
			}
			switch strings.ToUpper(nullText) {
			case "YES":
				nullable = "nullable"
			case "NO":
				nullable = "not_null"
			default:
				nullable = "unknown"
			}
		}
		columns = append(columns, driver.CatalogColumn{Name: name, NativeType: native, Nullable: nullable})
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(columns) == 0 {
		return result, fmt.Errorf("no complete metadata evidence")
	}
	return driver.CatalogRelation{Reference: driver.CatalogReference{ID: ref.ID, Parts: append([]string(nil), ref.Parts...)}, Outcome: "found", ColumnsComplete: true, Columns: columns}, nil
}
func text(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}
