// Package fixture owns the neutral logical table data loaded by S2SBench's
// engine adapters. It describes the physical data world that each supported
// benchmark engine must reproduce.
package fixture

import (
	"fmt"
	"strconv"
	"strings"

	conformance "github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
)

type LogicalType string

const (
	String   LogicalType = "string"
	Integer  LogicalType = "integer"
	Float    LogicalType = "float"
	Decimal  LogicalType = "decimal"
	Boolean  LogicalType = "boolean"
	Date     LogicalType = "date"
	DateTime LogicalType = "datetime"
)

type Column struct {
	Name     string
	Type     LogicalType
	Nullable bool
}

type Table struct {
	Name       string
	Columns    []Column
	KeyColumns []string
	Rows       [][]any
}

type Dataset struct {
	ID     conformance.ID
	Tables []Table
}

// Dialect translates only physical fixture concerns. It is deliberately a
// data-loading contract rather than a Renderer authority: Renderers compile queries;
// fixture dialects create deterministic test tables.
type Dialect interface {
	CreateTable(Table) (string, error)
}

func Statements(id conformance.ID, dialect Dialect) ([]string, error) {
	if dialect == nil {
		return nil, fmt.Errorf("fixture dialect is required")
	}
	dataset, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown canonical fixture %q", id)
	}
	if err := Validate(dataset); err != nil {
		return nil, err
	}
	statements := make([]string, 0, len(dataset.Tables)*3)
	for _, table := range dataset.Tables {
		statements = append(statements, "DROP TABLE IF EXISTS analytics."+table.Name)
		create, err := dialect.CreateTable(table)
		if err != nil {
			return nil, fmt.Errorf("fixture %q table %q: %w", id, table.Name, err)
		}
		statements = append(statements, create)
		if len(table.Rows) != 0 {
			insert, err := insertStatement(table)
			if err != nil {
				return nil, fmt.Errorf("fixture %q table %q: %w", id, table.Name, err)
			}
			statements = append(statements, insert)
		}
	}
	return statements, nil
}

func Validate(dataset Dataset) error {
	if dataset.ID == "" {
		return fmt.Errorf("fixture ID is required")
	}
	seenTables := map[string]struct{}{}
	for _, table := range dataset.Tables {
		if table.Name == "" || len(table.Columns) == 0 {
			return fmt.Errorf("fixture %q has an incomplete table", dataset.ID)
		}
		if _, exists := seenTables[table.Name]; exists {
			return fmt.Errorf("fixture %q repeats table %q", dataset.ID, table.Name)
		}
		seenTables[table.Name] = struct{}{}
		columns := map[string]Column{}
		for _, column := range table.Columns {
			if column.Name == "" || !knownType(column.Type) {
				return fmt.Errorf("fixture %q table %q has invalid column %#v", dataset.ID, table.Name, column)
			}
			if _, exists := columns[column.Name]; exists {
				return fmt.Errorf("fixture %q table %q repeats column %q", dataset.ID, table.Name, column.Name)
			}
			columns[column.Name] = column
		}
		for _, key := range table.KeyColumns {
			if _, exists := columns[key]; !exists {
				return fmt.Errorf("fixture %q table %q key column %q does not exist", dataset.ID, table.Name, key)
			}
		}
		for rowIndex, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("fixture %q table %q row %d has %d values, want %d", dataset.ID, table.Name, rowIndex, len(row), len(table.Columns))
			}
			for columnIndex, value := range row {
				if value == nil && !table.Columns[columnIndex].Nullable {
					return fmt.Errorf("fixture %q table %q row %d column %q is unexpectedly NULL", dataset.ID, table.Name, rowIndex, table.Columns[columnIndex].Name)
				}
				if value != nil && !validValue(table.Columns[columnIndex].Type, value) {
					return fmt.Errorf("fixture %q table %q row %d column %q has %T, incompatible with %s", dataset.ID, table.Name, rowIndex, table.Columns[columnIndex].Name, value, table.Columns[columnIndex].Type)
				}
			}
		}
	}
	return nil
}

func knownType(value LogicalType) bool {
	switch value {
	case String, Integer, Float, Decimal, Boolean, Date, DateTime:
		return true
	default:
		return false
	}
}

func validValue(logicalType LogicalType, value any) bool {
	switch logicalType {
	case String, Date, DateTime:
		_, ok := value.(string)
		return ok
	case Integer:
		switch value.(type) {
		case int, int64:
			return true
		}
	case Float, Decimal:
		switch value.(type) {
		case int, int64, float64:
			return true
		}
	case Boolean:
		_, ok := value.(bool)
		return ok
	}
	return false
}

func insertStatement(table Table) (string, error) {
	rows := make([]string, len(table.Rows))
	for rowIndex, row := range table.Rows {
		values := make([]string, len(row))
		for columnIndex, value := range row {
			literalValue, err := literal(value)
			if err != nil {
				return "", fmt.Errorf("row %d column %q: %w", rowIndex, table.Columns[columnIndex].Name, err)
			}
			values[columnIndex] = literalValue
		}
		rows[rowIndex] = "(" + strings.Join(values, ",") + ")"
	}
	return "INSERT INTO analytics." + table.Name + " VALUES " + strings.Join(rows, ","), nil
}

func literal(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "NULL", nil
	case string:
		return "'" + strings.ReplaceAll(typed, "'", "''") + "'", nil
	case bool:
		if typed {
			return "TRUE", nil
		}
		return "FALSE", nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported canonical fixture value %T", value)
	}
}
