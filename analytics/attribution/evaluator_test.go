package attribution_test

import (
	"encoding/json"
	"testing"

	"github.com/meaningforge/metis/analytics/attribution"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

func TestEvaluateAdditiveReconcilesAndCanonicalizesMembers(t *testing.T) {
	evidence := attribution.DimensionEvidence{
		Dimension: "dimension:sales.orders.region",
		Schema:    additiveSchema("dimension:sales.orders.region", ossie.DataTypeString),
		Rows: [][]any{
			{nil, "1", "0", "-1", "2", "-50"},
			{"west", "2", "5", "3", "2", "150"},
		},
	}
	result, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	dimension := result.Dimensions[0]
	if dimension.Additive == nil || dimension.Ratio != nil || dimension.Additive.Summary.TotalDelta != "2" || !dimension.Additive.Summary.Reconciled {
		t.Fatalf("result = %#v", dimension)
	}
	if string(dimension.Additive.Segments[0].Value) != `"west"` || string(dimension.Additive.Segments[1].Value) != "null" {
		t.Fatalf("segment order = %#v", dimension.Additive.Segments)
	}
	if got := *dimension.Additive.Segments[0].ContributionPct; got != "150" {
		t.Fatalf("contribution = %q", got)
	}
}

func TestEvaluateAdditiveEmptyPopulationReturnsZeroIdentity(t *testing.T) {
	result, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{{Dimension: "region", Schema: additiveSchema("region", ossie.DataTypeString)}})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Dimensions[0].Additive.Summary
	if summary.BaselineTotal != "0" || summary.CurrentTotal != "0" || summary.TotalDelta != "0" || summary.ContributionDefined || !summary.Reconciled {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestEvaluateRatioProducesTypedDefinedEvidence(t *testing.T) {
	row := []any{"a", true, true, "1", "2", "3", "4", "0.5", "0.75", "1", "1", true, true, true, "0.25", "0", "0", "0", "0.25", "0.5", "0.75", "0.25", "0.25", "0", true}
	result, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyRatio), []attribution.DimensionEvidence{{Dimension: "segment", Schema: ratioSchema("segment"), Rows: [][]any{row}}})
	if err != nil {
		t.Fatal(err)
	}
	ratio := result.Dimensions[0].Ratio
	if ratio == nil || !ratio.Summary.AttributionDefined || !ratio.Summary.Reconciled || *ratio.Summary.BaselineNumerator != "1" || *ratio.Summary.CurrentRatio != "0.75" || *ratio.Segments[0].TotalEffect != "0.25" {
		t.Fatalf("ratio = %#v", ratio)
	}
}

func TestEvaluateRatioEmptyPopulationPreservesUndefinedState(t *testing.T) {
	result, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyRatio), []attribution.DimensionEvidence{{Dimension: "segment", Schema: ratioSchema("segment")}})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Dimensions[0].Ratio.Summary
	if summary.AttributionDefined || !summary.Reconciled || summary.BaselineNumerator != nil || summary.BaselineRatio != nil {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestEvaluateRatioRejectsImpossiblePopulationEvidence(t *testing.T) {
	base := []any{"a", false, true, "1", "0", "1", "2", nil, "0.5", "0", "1", false, true, true, "0", "0", "0.5", "0", "0.5", nil, "0.5", nil, nil, nil, false}
	evidence := attribution.DimensionEvidence{Dimension: "segment", Schema: ratioSchema("segment"), Rows: [][]any{base}}
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyRatio), []attribution.DimensionEvidence{evidence}); err == nil {
		t.Fatal("non-zero absent-period evidence unexpectedly evaluated")
	}
	base[3] = "0"
	base[1], base[2] = false, false
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyRatio), []attribution.DimensionEvidence{evidence}); err == nil {
		t.Fatal("segment absent from both periods unexpectedly evaluated")
	}
}

func TestEvaluateRejectsDuplicateAndInconsistentEvidence(t *testing.T) {
	duplicate := attribution.DimensionEvidence{Dimension: "region", Schema: additiveSchema("region", ossie.DataTypeString), Rows: [][]any{{"x", "0", "1", "1", "2", "50"}, {"x", "0", "1", "1", "2", "50"}}}
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{duplicate}); err == nil {
		t.Fatal("duplicate member unexpectedly evaluated")
	}
	inconsistent := attribution.DimensionEvidence{Dimension: "region", Schema: additiveSchema("region", ossie.DataTypeString), Rows: [][]any{{"x", "0", "1", "2", "2", "100"}}}
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{inconsistent}); err == nil {
		t.Fatal("inconsistent delta unexpectedly evaluated")
	}
}

func TestEvaluateRejectsNumericallyEqualDecimalMembers(t *testing.T) {
	evidence := attribution.DimensionEvidence{Dimension: "amount", Schema: additiveSchema("amount", ossie.DataTypeDecimal), Rows: [][]any{{"1.0", "0", "1", "1", "2", "50"}, {"1.00", "0", "1", "1", "2", "50"}}}
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{evidence}); err == nil {
		t.Fatal("numerically duplicate decimal members unexpectedly evaluated")
	}
}

func TestEvaluateRejectsBinaryFloatNumericEvidence(t *testing.T) {
	evidence := attribution.DimensionEvidence{Dimension: "region", Schema: additiveSchema("region", ossie.DataTypeDecimal), Rows: [][]any{{"1.5", float64(1), "2", "1", "1", "100"}}}
	if _, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyAdditive), []attribution.DimensionEvidence{evidence}); err == nil {
		t.Fatal("binary float evidence unexpectedly evaluated")
	}
}

func TestResultNullableDecimalFieldsSerializeAsExplicitNull(t *testing.T) {
	result, err := attribution.Evaluate(descriptor(attribution.AttributionStrategyRatio), []attribution.DimensionEvidence{{Dimension: "segment", Schema: ratioSchema("segment")}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoded) || !contains(string(encoded), `"baseline_ratio":null`) {
		t.Fatalf("JSON = %s", encoded)
	}
}

func descriptor(strategy attribution.AttributionStrategy) attribution.Descriptor {
	return attribution.Descriptor{AnalysisID: "analysis", Metric: "metric:sales.value", TimeDimension: "dimension:sales.orders.time", Baseline: attribution.AttributionPeriod{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"}, Current: attribution.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"}, Strategy: strategy}
}

func additiveSchema(dimension string, memberType ossie.DataType) artifact.OutputSchema {
	return artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: dimension, Kind: artifact.OutputDimension, Datatype: memberType},
		{Name: "baseline_value", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "current_value", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "delta", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "total_delta", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "contribution_pct", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	}}
}

func ratioSchema(dimension string) artifact.OutputSchema {
	names := []string{dimension, "baseline_present", "current_present", "baseline_numerator", "baseline_denominator", "current_numerator", "current_denominator", "baseline_rate", "current_rate", "baseline_weight", "current_weight", "baseline_defined", "current_defined", "segment_defined", "rate_effect", "mix_effect", "entry_effect", "exit_effect", "segment_effect", "baseline_ratio", "current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"}
	columns := make([]artifact.OutputColumn, len(names))
	for i, name := range names {
		columns[i] = artifact.OutputColumn{Name: name, Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal}
	}
	columns[0] = artifact.OutputColumn{Name: dimension, Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString}
	for _, index := range []int{1, 2, 11, 12, 13, 24} {
		columns[index].Kind = artifact.OutputDimension
		columns[index].Datatype = ossie.DataTypeBoolean
	}
	return artifact.OutputSchema{Columns: columns}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
