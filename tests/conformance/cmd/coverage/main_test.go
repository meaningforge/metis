package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestRenderIsDeterministicAcrossRegistryOrder(t *testing.T) {
	forward := append([]scenarios.Scenario(nil), scenarios.Core...)
	reversed := append([]scenarios.Scenario(nil), scenarios.Core...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}

	if !bytes.Equal(render(forward), render(reversed)) {
		t.Fatal("coverage report depends on scenario registration order")
	}
}

func TestRenderExposesEvidenceDepth(t *testing.T) {
	report := string(render(scenarios.Core))
	scenarioCount := len(scenarios.Core)
	for _, expected := range []string{
		fmt.Sprintf("Canonical scenarios: **%d**", scenarioCount),
		fmt.Sprintf("Compiler contracts required: **%d**", scenarioCount),
		fmt.Sprintf("Result contracts required: **%d**", scenarioCount),
		"Result contracts pending: **0**",
		"| Columns | Comparison | Expected rows |",
		"`revenue:number`",
		"ordered",
		"| `simple_metric` | metric | `commerce` |",
		"| `derived_metric` | composition | `commerce` |",
		"| `distinct_dimension_values` | query_intent | `distinct_values` |",
		"| `metric_definition_filter_pre_aggregation` | filter | `definition_filters` |",
		"| `semi_additive_first_with_tie_break` | composition | `semi_additive_tie_break` |",
		"| `conversion_rate_by_campaign` | composition | `conversion` |",
		"| `custom_calendar_fiscal_quarter_to_date` | time | `custom_calendar_grain_to_date` |",
		"| `temporal_join_point_in_time` | join | `temporal_relationship` |",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("coverage report missing %q", expected)
		}
	}
}
