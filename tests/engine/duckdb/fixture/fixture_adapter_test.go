//go:build duckdb

package fixture

import (
	"strings"
	"testing"

	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

func TestDuckDBFixtureDialectMapsOnlyPhysicalConcerns(t *testing.T) {
	statement, err := (duckDBFixtureDialect{}).CreateTable(enginefixture.Table{
		Name: "events",
		Columns: []enginefixture.Column{
			{Name: "amount", Type: enginefixture.Decimal, Nullable: true},
			{Name: "event_time", Type: enginefixture.DateTime},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"amount DECIMAL(20,12)", "event_time TIMESTAMP"} {
		if !strings.Contains(statement, expected) {
			t.Fatalf("DuckDB fixture DDL = %q, want %q", statement, expected)
		}
	}
}
