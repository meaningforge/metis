package scenarios

import "testing"

func TestSemanticCompositionMatrixHasSharedCompilerAndResultContracts(t *testing.T) {
	names := []string{
		"multi_source_derived_at_grain",
		"cumulative_metric_with_source_filter",
		"semi_additive_with_ordinary_metric",
		"conversion_rate_filtered_campaign",
		"shared_grain_derived_with_source_metric",
		"custom_calendar_cumulative_with_filter",
		"semantic_extension_derived_metric",
		"metric_filter_multi_metric",
	}
	for _, name := range names {
		scenario, ok := ByName(name)
		if !ok {
			t.Fatalf("semantic composition scenario %q is not registered", name)
		}
		if scenario.Verification.Compiler != ContractRequired {
			t.Fatalf("semantic composition scenario %q compiler contract = %q, want required", name, scenario.Verification.Compiler)
		}
		if scenario.Verification.Result != ContractRequired || scenario.ExpectedResult == nil {
			t.Fatalf("semantic composition scenario %q does not require canonical result correctness", name)
		}
	}
}
