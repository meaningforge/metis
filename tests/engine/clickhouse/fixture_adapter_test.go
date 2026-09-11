package clickhouse_test

import (
	"strings"
	"testing"

	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

func TestClickHouseFixtureDialectMapsOnlyPhysicalConcerns(t *testing.T) {
	statement, err := (clickHouseFixtureDialect{}).CreateTable(enginefixture.Table{
		Name: "events",
		Columns: []enginefixture.Column{
			{Name: "amount", Type: enginefixture.Decimal, Nullable: true},
			{Name: "event_time", Type: enginefixture.DateTime},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"amount Nullable(Float64)", "event_time DateTime", "ENGINE = Memory"} {
		if !strings.Contains(statement, expected) {
			t.Fatalf("ClickHouse fixture DDL = %q, want %q", statement, expected)
		}
	}
}
