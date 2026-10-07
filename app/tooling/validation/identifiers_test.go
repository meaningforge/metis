package validation

import "testing"

func TestDuckDBIdentifierCaseSemantics(t *testing.T) {
	if !columnMatches("duckdb", "Region", "region") || columnMatches("duckdb", "Ä", "ä") || columnMatches("clickhouse", "Region", "region") {
		t.Fatal("incorrect backend identifier comparison")
	}
}
