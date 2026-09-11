package doris_test

import (
	"strings"
	"testing"

	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

func TestDorisFixtureDialectMapsOnlyPhysicalConcerns(t *testing.T) {
	statement, err := (dorisFixtureDialect{}).CreateTable(enginefixture.Table{
		Name:       "events",
		KeyColumns: []string{"event_time"},
		Columns: []enginefixture.Column{
			{Name: "amount", Type: enginefixture.Decimal, Nullable: true},
			{Name: "event_time", Type: enginefixture.DateTime},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"amount DECIMAL(20,12) NULL", "event_time DATETIME", "DUPLICATE KEY(event_time)", "DISTRIBUTED BY HASH(event_time)"} {
		if !strings.Contains(statement, expected) {
			t.Fatalf("Doris fixture DDL = %q, want %q", statement, expected)
		}
	}
}
