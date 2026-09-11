package harness

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestGroupByFixturePreservesFirstSeenFixtureAndScenarioOrder(t *testing.T) {
	shared := []scenarios.Scenario{
		{Name: "commerce_one", Fixture: fixtures.Commerce},
		{Name: "distinct_one", Fixture: fixtures.DistinctValues},
		{Name: "commerce_two", Fixture: fixtures.Commerce},
		{Name: "definition_one", Fixture: fixtures.DefinitionFilters},
	}

	order, grouped := groupByFixture(shared)
	if want := []fixtures.ID{fixtures.Commerce, fixtures.DistinctValues, fixtures.DefinitionFilters}; !reflect.DeepEqual(order, want) {
		t.Fatalf("fixture order = %v, want %v", order, want)
	}
	if got, want := scenarioNames(grouped[fixtures.Commerce]), []string{"commerce_one", "commerce_two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commerce scenarios = %v, want %v", got, want)
	}
}

func TestCompareResultHonorsOrderedAndUnorderedContracts(t *testing.T) {
	columns := []scenarios.ResultColumn{{Name: "region", ValueKind: scenarios.ResultString}}
	a := mustResultValue(t, scenarios.ResultString, "APAC")
	b := mustResultValue(t, scenarios.ResultString, "EU")
	actual := scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{b}, {a}}}

	unordered := &scenarios.ResultExpectation{
		ResultSet:  scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{a}, {b}}},
		Comparison: scenarios.ResultUnordered,
	}
	if err := compareResult(unordered, actual); err != nil {
		t.Fatalf("unordered comparison failed: %v", err)
	}

	ordered := &scenarios.ResultExpectation{
		ResultSet:  unordered.ResultSet,
		Comparison: scenarios.ResultOrdered,
	}
	if err := compareResult(ordered, actual); err == nil {
		t.Fatal("ordered comparison accepted reversed rows")
	}
}

func TestCompareResultRejectsSchemaKindAndDuplicateMismatches(t *testing.T) {
	columns := []scenarios.ResultColumn{{Name: "orders_count", ValueKind: scenarios.ResultInteger}}
	one := mustResultValue(t, scenarios.ResultInteger, "1")
	expected := &scenarios.ResultExpectation{
		ResultSet:  scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{one}, {one}}},
		Comparison: scenarios.ResultUnordered,
	}

	wrongSchema := scenarios.ResultSet{
		Columns: []scenarios.ResultColumn{{Name: "orders_count", ValueKind: scenarios.ResultString}},
		Rows:    []scenarios.ResultRow{{mustResultValue(t, scenarios.ResultString, "1")}, {mustResultValue(t, scenarios.ResultString, "1")}},
	}
	if err := compareResult(expected, wrongSchema); err == nil {
		t.Fatal("comparison accepted a physical string for an integer column")
	}

	missingDuplicate := scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{one}}}
	if err := compareResult(expected, missingDuplicate); err == nil {
		t.Fatal("comparison ignored a duplicate-row mismatch")
	}
}

func TestCompareResultUsesBoundedNumberTolerance(t *testing.T) {
	columns := []scenarios.ResultColumn{{Name: "average", ValueKind: scenarios.ResultNumber}}
	expected := &scenarios.ResultExpectation{
		ResultSet: scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{
			mustResultValue(t, scenarios.ResultNumber, "116.666666666667"),
		}}},
		Comparison: scenarios.ResultUnordered,
	}
	withinTolerance := scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{
		mustResultValue(t, scenarios.ResultNumber, "116.666666666666"),
	}}}
	if err := compareResult(expected, withinTolerance); err != nil {
		t.Fatalf("comparison rejected engine decimal rounding: %v", err)
	}
	beyondTolerance := scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{{
		mustResultValue(t, scenarios.ResultNumber, "116.6666666"),
	}}}
	if err := compareResult(expected, beyondTolerance); err == nil {
		t.Fatal("comparison accepted a numeric difference beyond tolerance")
	}
}

func TestCompareResultFindsUnorderedNumericMatchingWithoutGreedyBias(t *testing.T) {
	columns := []scenarios.ResultColumn{{Name: "value", ValueKind: scenarios.ResultNumber}}
	expected := &scenarios.ResultExpectation{
		ResultSet: scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{
			{mustResultValue(t, scenarios.ResultNumber, "0")},
			{mustResultValue(t, scenarios.ResultNumber, "0.0000000015")},
		}},
		Comparison: scenarios.ResultUnordered,
	}
	actual := scenarios.ResultSet{Columns: columns, Rows: []scenarios.ResultRow{
		{mustResultValue(t, scenarios.ResultNumber, "0.00000000075")},
		{mustResultValue(t, scenarios.ResultNumber, "-0.00000000075")},
	}}
	if err := compareResult(expected, actual); err != nil {
		t.Fatalf("unordered maximum matching failed: %v", err)
	}
}

func mustResultValue(t *testing.T, kind scenarios.ResultValueKind, raw string) scenarios.ResultValue {
	t.Helper()
	value, err := scenarios.ParseResultValue(kind, raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func scenarioNames(values []scenarios.Scenario) []string {
	names := make([]string, len(values))
	for i, scenario := range values {
		names[i] = scenario.Name
	}
	return names
}
