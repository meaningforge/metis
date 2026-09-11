package scenarios

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestAdversarialCommerceCorpusRetainsDiscriminatingSignals(t *testing.T) {
	if len(dataEdgeScenarios) == 0 {
		t.Fatal("adversarial commerce corpus is empty")
	}
	hasNull, hasZero, hasNegative, hasOrdered := false, false, false, false
	for _, scenario := range dataEdgeScenarios {
		if scenario.Fixture != fixtures.CommerceAdversarial {
			t.Fatalf("adversarial scenario %q uses fixture %q", scenario.Name, scenario.Fixture)
		}
		if scenario.ExpectedResult == nil {
			t.Fatalf("adversarial scenario %q lacks a result oracle", scenario.Name)
		}
		hasOrdered = hasOrdered || scenario.ExpectedResult.Comparison == ResultOrdered
		for _, row := range scenario.ExpectedResult.Rows {
			for _, value := range row {
				hasNull = hasNull || value.Null
				if !value.Null && (value.ValueKind == ResultNumber || value.ValueKind == ResultInteger) {
					hasZero = hasZero || value.Canonical == "0"
					hasNegative = hasNegative || strings.HasPrefix(value.Canonical, "-")
				}
			}
		}
	}
	if !hasNull || !hasZero || !hasNegative || !hasOrdered {
		t.Fatalf("adversarial signals: null=%t zero=%t negative=%t ordered=%t", hasNull, hasZero, hasNegative, hasOrdered)
	}
}

func TestScenarioRegistryContract(t *testing.T) {
	seen := make(map[string]struct{}, len(Core))
	for _, scenario := range Core {
		if scenario.Name == "" {
			t.Fatal("scenario name must not be empty")
		}
		if _, ok := seen[scenario.Name]; ok {
			t.Fatalf("duplicate scenario name %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
		if scenario.Category == "" {
			t.Fatalf("scenario %q must declare a semantic category", scenario.Name)
		}
		if len(scenario.Requires) == 0 {
			t.Fatalf("scenario %q must declare required semantic capabilities", scenario.Name)
		}
		if scenario.Fixture == "" {
			t.Fatalf("scenario %q must declare a canonical fixture", scenario.Name)
		}
		definition, ok := fixtures.Lookup(scenario.Fixture)
		if !ok {
			t.Fatalf("scenario %q references unknown fixture %q", scenario.Name, scenario.Fixture)
		}
		if scenario.Query.Project != definition.Project || scenario.Query.Model != definition.Model {
			t.Fatalf("scenario %q namespace = %s/%s, fixture namespace = %s/%s", scenario.Name, scenario.Query.Project, scenario.Query.Model, definition.Project, definition.Model)
		}
		if scenario.Verification.Compiler != ContractRequired {
			t.Fatalf("scenario %q compiler contract = %q, want %q", scenario.Name, scenario.Verification.Compiler, ContractRequired)
		}
		if len(scenario.ExpectedQuery.Fragments) == 0 {
			t.Fatalf("scenario %q must declare a compiler shape expectation", scenario.Name)
		}
		switch scenario.Verification.Result {
		case ContractRequired:
			if scenario.ExpectedResult == nil {
				t.Fatalf("scenario %q requires result verification without an expectation", scenario.Name)
			}
			if scenario.Verification.Reason != "" {
				t.Fatalf("scenario %q has a result expectation and a pending reason", scenario.Name)
			}
			wantColumns := len(scenario.Query.Dimensions) + len(scenario.Query.Metrics)
			if len(scenario.ExpectedResult.Columns) != wantColumns {
				t.Fatalf("scenario %q result columns = %d, want %d", scenario.Name, len(scenario.ExpectedResult.Columns), wantColumns)
			}
			wantComparison := ResultUnordered
			if len(scenario.Query.OrderBy) != 0 {
				wantComparison = ResultOrdered
			}
			if scenario.ExpectedResult.Comparison != wantComparison {
				t.Fatalf("scenario %q comparison = %q, want %q", scenario.Name, scenario.ExpectedResult.Comparison, wantComparison)
			}
			for rowIndex, row := range scenario.ExpectedResult.Rows {
				if len(row) != wantColumns {
					t.Fatalf("scenario %q row %d values = %d, want %d", scenario.Name, rowIndex, len(row), wantColumns)
				}
				for columnIndex, value := range row {
					if value.ValueKind != scenario.ExpectedResult.Columns[columnIndex].ValueKind {
						t.Fatalf("scenario %q row %d column %d kind = %q, want %q", scenario.Name, rowIndex, columnIndex, value.ValueKind, scenario.ExpectedResult.Columns[columnIndex].ValueKind)
					}
				}
			}
		case ContractPending:
			if scenario.ExpectedResult != nil {
				t.Fatalf("scenario %q has a pending result contract with an expectation", scenario.Name)
			}
			if strings.TrimSpace(scenario.Verification.Reason) == "" {
				t.Fatalf("scenario %q must explain why result verification is pending", scenario.Name)
			}
		default:
			t.Fatalf("scenario %q has unknown result contract status %q", scenario.Name, scenario.Verification.Result)
		}
		lower := strings.ToLower(scenario.Name)
		for _, backend := range []string{"clickhouse", "doris", "snowflake", "bigquery", "databricks", "postgres", "trino"} {
			if strings.Contains(lower, backend) {
				t.Fatalf("scenario %q is backend-specific; shared scenarios must be engine-neutral", scenario.Name)
			}
		}
	}
	if len(compilerExpectations) != len(Core) {
		t.Fatalf("compiler expectations = %d, canonical scenarios = %d", len(compilerExpectations), len(Core))
	}
	for name := range compilerExpectations {
		if _, ok := seen[name]; !ok {
			t.Fatalf("compiler expectation %q has no canonical scenario", name)
		}
	}
}

func TestResultContractsCarryLogicalColumnsAndTypedValues(t *testing.T) {
	metrics, ok := ByName("multiple_metrics_same_source")
	if !ok {
		t.Fatal("missing multiple_metrics_same_source")
	}
	wantMetricColumns := []ResultColumn{
		{Name: "revenue", ValueKind: ResultNumber},
		{Name: "orders_count", ValueKind: ResultInteger},
	}
	for i, want := range wantMetricColumns {
		if got := metrics.ExpectedResult.Columns[i]; got != want {
			t.Fatalf("metric column %d = %#v, want %#v", i, got, want)
		}
	}

	timeScenario, ok := ByName("time_month")
	if !ok {
		t.Fatal("missing time_month")
	}
	if got := timeScenario.ExpectedResult.Columns[0]; got != (ResultColumn{Name: "order_date", ValueKind: ResultDate}) {
		t.Fatalf("time column = %#v", got)
	}
	if got := timeScenario.ExpectedResult.Rows[0][1].Canonical; got != "300" {
		t.Fatalf("canonical number = %q, want 300", got)
	}

	ordered, ok := ByName("joined_dimension_filter_order_limit")
	if !ok || ordered.ExpectedResult.Comparison != ResultOrdered {
		t.Fatal("ORDER BY scenario does not require ordered result comparison")
	}
	unordered, ok := ByName("one_hop_join")
	if !ok || unordered.ExpectedResult.Comparison != ResultUnordered {
		t.Fatal("unordered scenario does not ignore presentation order")
	}
}

func TestParseResultValueNormalizesBackendRepresentations(t *testing.T) {
	integer, err := ParseResultValue(ResultInteger, "0002")
	if err != nil || integer.Canonical != "2" {
		t.Fatalf("integer = %#v, err = %v", integer, err)
	}
	number, err := ParseResultValue(ResultNumber, "1.00")
	if err != nil || number.Canonical != "1" {
		t.Fatalf("number = %#v, err = %v", number, err)
	}
	boolean, err := ParseResultValue(ResultBoolean, "1")
	if err != nil || boolean.Canonical != "true" {
		t.Fatalf("boolean = %#v, err = %v", boolean, err)
	}
	datetime, err := ParseResultValue(ResultDateTime, "2026-01-01T08:00:00+08:00")
	if err != nil || datetime.Canonical != "2026-01-01T08:00:00+08:00" {
		t.Fatalf("datetime = %#v, err = %v", datetime, err)
	}
}

func TestNumbersWithinToleranceUsesExactBoundedArithmetic(t *testing.T) {
	if !NumbersWithinTolerance("60.000000000000000001", "60") {
		t.Fatal("exact difference inside tolerance was rejected")
	}
	if NumbersWithinTolerance("60.000000001000000001", "60") {
		t.Fatal("difference beyond tolerance was accepted")
	}
	if NumbersWithinTolerance("NaN", "0") {
		t.Fatal("non-rational input was accepted")
	}
}

func TestExecutionCorpusIsSharedRegistryOnly(t *testing.T) {
	for _, scenario := range ExecutionCore() {
		if scenario.Category == "" || len(scenario.Requires) == 0 {
			t.Fatalf("execution scenario %q must carry shared semantic metadata", scenario.Name)
		}
		registered, ok := ByName(scenario.Name)
		if !ok || registered.ExpectedResult == nil {
			t.Fatalf("execution scenario %q must be defined by the canonical registry", scenario.Name)
		}
	}
}
