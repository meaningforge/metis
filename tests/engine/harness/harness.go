package harness

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/reference"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

// Backend contains execution-specific behavior. Capability declarations are
// resolved from the canonical real-engine registry.
type Backend struct {
	Name          string
	Prepare       func(t *testing.T, fixture fixtures.ID)
	OpenExecution func(t *testing.T) *ProductionExecution
}

// RunSharedExecutionContract executes the canonical executable scenario corpus
// against one backend. Scenario importance is deliberately not consulted here:
// execution support is derived from the registered backend capabilities plus
// sparse target exceptions.
func RunSharedExecutionContract(t *testing.T, backend Backend) {
	t.Helper()
	if backend.Name == "" {
		t.Fatal("engine backend name is required")
	}
	capabilities, ok := evidence.RealEngineCapabilities(backend.Name)
	if !ok {
		t.Fatalf("%s backend is not registered for real-engine conformance evidence", backend.Name)
	}
	if backend.Prepare == nil || backend.OpenExecution == nil {
		t.Fatalf("%s backend must provide Prepare and production OpenExecution", backend.Name)
	}

	fixtureOrder, byFixture := groupByFixture(scenarios.ExecutionCore())
	for _, fixture := range fixtureOrder {
		fixture := fixture
		t.Run(string(fixture), func(t *testing.T) {
			backend.Prepare(t, fixture)
			execution := backend.OpenExecution(t)
			if execution == nil {
				t.Fatalf("%s backend returned nil production Execution", backend.Name)
			}
			for _, scenario := range byFixture[fixture] {
				scenario := scenario
				t.Run(scenario.Name, func(t *testing.T) {
					expected := scenario.ExpectedResult
					if c, ok := reference.ByScenario(scenario.Name); ok {
						expectation, err := reference.TargetExpectationFor(c, backend.Name, capabilities)
						if err != nil {
							t.Fatalf("resolve reference scenario %q for %s: %v", scenario.Name, backend.Name, err)
						}
						switch expectation.Status {
						case reference.TargetUnsupported:
							t.Skipf("%s does not support reference scenario %s: %s", backend.Name, scenario.Name, expectation.Reason)
						case reference.TargetIntentionalDifference:
							expected = expectation.ExpectedResult
						case reference.TargetRequired:
							// Continue with the canonical expected result.
						default:
							t.Fatalf("reference scenario %q resolved unknown target status %q for %s", scenario.Name, expectation.Status, backend.Name)
						}
					}
					if missing := capabilities.Missing(scenario.Requires); len(missing) != 0 {
						t.Fatalf("%s does not declare required capabilities for %q: %v", backend.Name, scenario.Name, missing)
					}
					if expected == nil {
						t.Fatalf("shared scenario %q requires execution without a result expectation", scenario.Name)
					}
					actual := execution.execute(t, scenario)
					if err := compareResult(expected, actual); err != nil {
						t.Fatalf("%s result mismatch for %s: %v", backend.Name, scenario.Name, err)
					}
				})
			}
		})
	}
	runRegisteredMetricScaleContract(t, backend)
	runDataPolicyContract(t, backend)
}

func runRegisteredMetricScaleContract(t *testing.T, backend Backend) {
	t.Helper()
	t.Run(string(enginefixture.MetricScale), func(t *testing.T) {
		backend.Prepare(t, enginefixture.MetricScale)
		execution := backend.OpenExecution(t)
		if execution == nil {
			t.Fatalf("%s backend returned nil production Execution", backend.Name)
		}
		actual := execution.RunRegisteredMetricScale(t)
		value, err := scenarios.ParseResultValue(scenarios.ResultNumber, "60")
		if err != nil {
			t.Fatal(err)
		}
		expected := &scenarios.ResultExpectation{
			ResultSet: scenarios.ResultSet{
				Columns: []scenarios.ResultColumn{{Name: "revenue", ValueKind: scenarios.ResultNumber}},
				Rows:    []scenarios.ResultRow{{value}},
			},
			Comparison: scenarios.ResultUnordered,
		}
		if err := compareResult(expected, actual); err != nil {
			t.Fatalf("%s registered metric-scale result mismatch: %v", backend.Name, err)
		}
	})
}

func groupByFixture(shared []scenarios.Scenario) ([]fixtures.ID, map[fixtures.ID][]scenarios.Scenario) {
	order := make([]fixtures.ID, 0)
	grouped := make(map[fixtures.ID][]scenarios.Scenario)
	for _, scenario := range shared {
		if _, seen := grouped[scenario.Fixture]; !seen {
			order = append(order, scenario.Fixture)
		}
		grouped[scenario.Fixture] = append(grouped[scenario.Fixture], scenario)
	}
	return order, grouped
}

func compareResult(expected *scenarios.ResultExpectation, actual scenarios.ResultSet) error {
	if expected == nil {
		return fmt.Errorf("expected result is required")
	}
	if !reflect.DeepEqual(expected.Columns, actual.Columns) {
		return fmt.Errorf("columns differ\nexpected: %#v\nactual:   %#v", expected.Columns, actual.Columns)
	}
	if err := validateRows(expected.Columns, expected.Rows); err != nil {
		return fmt.Errorf("invalid expected rows: %w", err)
	}
	if err := validateRows(actual.Columns, actual.Rows); err != nil {
		return fmt.Errorf("invalid actual rows: %w", err)
	}

	if !resultRowsEqual(expected.Comparison, expected.Rows, actual.Rows) {
		want := encodedRows(expected.Rows)
		got := encodedRows(actual.Rows)
		return fmt.Errorf("rows differ (%s)\nexpected:\n%s\nactual:\n%s", expected.Comparison, strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
	return nil
}

func resultRowsEqual(comparison scenarios.ResultComparison, expected, actual []scenarios.ResultRow) bool {
	if len(expected) != len(actual) {
		return false
	}
	switch comparison {
	case scenarios.ResultOrdered:
		for index := range expected {
			if !resultRowEqual(expected[index], actual[index]) {
				return false
			}
		}
		return true
	case scenarios.ResultUnordered:
		// Maximum bipartite matching avoids greedy ambiguity when multiple
		// engine rows sit within the numeric tolerance of multiple expectations.
		matchedExpected := make([]int, len(actual))
		for index := range matchedExpected {
			matchedExpected[index] = -1
		}
		var match func(int, []bool) bool
		match = func(expectedIndex int, seen []bool) bool {
			for actualIndex := range actual {
				if seen[actualIndex] || !resultRowEqual(expected[expectedIndex], actual[actualIndex]) {
					continue
				}
				seen[actualIndex] = true
				if matchedExpected[actualIndex] == -1 || match(matchedExpected[actualIndex], seen) {
					matchedExpected[actualIndex] = expectedIndex
					return true
				}
			}
			return false
		}
		for expectedIndex := range expected {
			if !match(expectedIndex, make([]bool, len(actual))) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func resultRowEqual(expected, actual scenarios.ResultRow) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		want, got := expected[index], actual[index]
		if want.ValueKind != got.ValueKind || want.Null != got.Null {
			return false
		}
		if want.Null {
			continue
		}
		if want.ValueKind != scenarios.ResultNumber {
			if want.Canonical != got.Canonical {
				return false
			}
			continue
		}
		if !scenarios.NumbersWithinTolerance(want.Canonical, got.Canonical) {
			return false
		}
	}
	return true
}

func validateRows(columns []scenarios.ResultColumn, rows []scenarios.ResultRow) error {
	for rowIndex, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("row %d has %d values, want %d", rowIndex, len(row), len(columns))
		}
		for columnIndex, value := range row {
			if value.ValueKind != columns[columnIndex].ValueKind {
				return fmt.Errorf("row %d column %q has kind %q, want %q", rowIndex, columns[columnIndex].Name, value.ValueKind, columns[columnIndex].ValueKind)
			}
		}
	}
	return nil
}

func encodedRows(rows []scenarios.ResultRow) []string {
	encoded := make([]string, len(rows))
	for i, row := range rows {
		values := make([]string, len(row))
		for j, value := range row {
			if value.Null {
				values[j] = fmt.Sprintf("%s(NULL)", value.ValueKind)
				continue
			}
			values[j] = fmt.Sprintf("%s(%q)", value.ValueKind, value.Canonical)
		}
		encoded[i] = fmt.Sprintf("%q", values)
	}
	return encoded
}
