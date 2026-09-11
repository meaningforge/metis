package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/reference"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestRenderIsDeterministicAcrossTargetOrder(t *testing.T) {
	forward := append([]evidence.Target(nil), evidence.Targets...)
	reversed := append([]evidence.Target(nil), evidence.Targets...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	forwardReport, err := render(forward, scenarios.Core, scenarios.ExecutionCore(), reference.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	reversedReport, err := render(reversed, scenarios.Core, scenarios.ExecutionCore(), reference.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwardReport, reversedReport) {
		t.Fatal("target evidence depends on registry order")
	}
}

func TestRenderSeparatesEvidenceLayersFromProductionSupport(t *testing.T) {
	reportBytes, err := render(evidence.Targets, scenarios.Core, scenarios.ExecutionCore(), reference.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	report := string(reportBytes)
	compilerRequired := countContracts(scenarios.Core, func(s scenarios.Scenario) scenarios.ContractStatus { return s.Verification.Compiler })
	resultRequired := countContracts(scenarios.Core, func(s scenarios.Scenario) scenarios.ContractStatus { return s.Verification.Result })
	executionCount := len(scenarios.ExecutionCore())
	referenceRequired := 0
	for _, c := range reference.Baseline {
		if c.Importance == reference.ImportanceRequired {
			referenceRequired++
		}
	}
	for _, expected := range []string{
		fmt.Sprintf("Canonical compiler scenarios: **%d**", compilerRequired),
		fmt.Sprintf("Canonical result scenarios: **%d**", resultRequired),
		fmt.Sprintf("Shared real-engine execution scenarios: **%d**", executionCount),
		fmt.Sprintf("Required reference reality scenarios: **%d**", referenceRequired),
		"Required reference categories: **composition, dimension, filter, join, metric, query_intent, time**",
		"Required reference capabilities: **aggregation, conversion_metric, cumulative_metric, custom_calendar, dense_calendar, derived_metric, dimension, distinct_values, filter, metric_definition_filter, metric_filter, multi_source, offset_to_grain, ordering, ratio_metric, relationship, semi_additive_metric, semi_additive_null_skip, semi_additive_query_grain, semi_additive_rollup, semi_additive_tie_break, semi_additive_window_grouping, temporal_relationship, time_grain, time_offset_metric**",
		fmt.Sprintf("| DuckDB | `DUCKDB` | ✅ %d required scenarios | ✅ %d required; 0 unsupported; 0 intentional differences | not asserted |", compilerRequired, executionCount),
		fmt.Sprintf("| ClickHouse | `CLICKHOUSE` | ✅ %d required scenarios | ✅ %d required; 0 unsupported; 0 intentional differences | not asserted |", compilerRequired, executionCount),
		fmt.Sprintf("| Doris | `DORIS` | ✅ %d required scenarios | ✅ %d required; 0 unsupported; 0 intentional differences | not asserted |", compilerRequired, executionCount),
		"No reference exceptions are currently declared.",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("target evidence missing %q", expected)
		}
	}
}

func TestRenderRejectsUnknownRequiredReferenceScenario(t *testing.T) {
	cases := append([]reference.Case(nil), reference.Baseline...)
	cases = append(cases, reference.Case{
		Scenario:   "missing_canonical_scenario",
		Importance: reference.ImportanceRequired,
		Origin:     reference.OriginManualOracle,
	})
	_, err := render(evidence.Targets, scenarios.Core, scenarios.ExecutionCore(), cases)
	if err == nil || !strings.Contains(err.Error(), "is not canonical") {
		t.Fatalf("render error = %v, want unknown required reference failure", err)
	}
}

func TestRenderSurfacesSparseReferenceExceptions(t *testing.T) {
	cases := append([]reference.Case(nil), reference.Baseline...)
	for i := range cases {
		if cases[i].Scenario != "simple_metric" {
			continue
		}
		cases[i].TargetOverrides = map[string]reference.TargetExpectation{
			"CLICKHOUSE": {
				Status: reference.TargetUnsupported,
				Reason: "backend lacks the required execution primitive",
			},
		}
		break
	}

	reportBytes, err := render(evidence.Targets, scenarios.Core, scenarios.ExecutionCore(), cases)
	if err != nil {
		t.Fatal(err)
	}
	report := string(reportBytes)
	for _, expected := range []string{
		fmt.Sprintf("✅ %d required; 1 unsupported; 0 intentional differences", len(scenarios.ExecutionCore())-1),
		"| `CLICKHOUSE` | `simple_metric` | `unsupported` | backend lacks the required execution primitive |",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("target evidence missing %q", expected)
		}
	}
}

func TestRenderFailsClosedWhenNonReferenceScenarioIsNotExecutable(t *testing.T) {
	targets := []evidence.Target{{
		Name:         "Limited",
		Dialect:      "limited",
		RealEngine:   true,
		Capabilities: scenarios.CapabilitySet{},
	}}
	_, err := render(targets, scenarios.Core, scenarios.ExecutionCore(), nil)
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("render error = %v, want non-executable scenario failure", err)
	}
}

func countContracts(registry []scenarios.Scenario, status func(scenarios.Scenario) scenarios.ContractStatus) int {
	count := 0
	for _, scenario := range registry {
		if status(scenario) == scenarios.ContractRequired {
			count++
		}
	}
	return count
}
