package fixture

import (
	"strings"
	"testing"
)

type recordingDialect struct{}

func (recordingDialect) CreateTable(table Table) (string, error) {
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		columns[i] = column.Name + " " + string(column.Type)
		if column.Nullable {
			columns[i] += " NULL"
		}
	}
	return "CREATE TABLE analytics." + table.Name + " (" + strings.Join(columns, ", ") + ")", nil
}

func TestEveryCanonicalDatasetValidates(t *testing.T) {
	for _, id := range IDs() {
		id := id
		t.Run(string(id), func(t *testing.T) {
			dataset, ok := Lookup(id)
			if !ok {
				t.Fatal("dataset is not registered")
			}
			if err := Validate(dataset); err != nil {
				t.Fatal(err)
			}
			statements, err := Statements(id, recordingDialect{})
			if err != nil {
				t.Fatal(err)
			}
			if len(statements) < len(dataset.Tables)*2 {
				t.Fatalf("statements = %d, want at least %d", len(statements), len(dataset.Tables)*2)
			}
		})
	}
}

func TestCanonicalRowsAreEscapedAndNullable(t *testing.T) {
	dataset := Dataset{ID: "literal", Tables: []Table{{
		Name: "events",
		Columns: []Column{
			{Name: "label", Type: String},
			{Name: "amount", Type: Float, Nullable: true},
		},
		KeyColumns: []string{"label"},
		Rows:       [][]any{{"O'Reilly", nil}},
	}}}
	if err := Validate(dataset); err != nil {
		t.Fatal(err)
	}
	statement, err := insertStatement(dataset.Tables[0])
	if err != nil {
		t.Fatal(err)
	}
	if statement != "INSERT INTO analytics.events VALUES ('O''Reilly',NULL)" {
		t.Fatalf("insert statement = %q", statement)
	}
}

func TestCanonicalRowsRejectLogicalTypeMismatch(t *testing.T) {
	dataset := Dataset{ID: "invalid", Tables: []Table{{
		Name:       "events",
		Columns:    []Column{{Name: "event_time", Type: DateTime}},
		KeyColumns: []string{"event_time"},
		Rows:       [][]any{{42}},
	}}}
	if err := Validate(dataset); err == nil || !strings.Contains(err.Error(), "incompatible with datetime") {
		t.Fatalf("validation error = %v", err)
	}
}
