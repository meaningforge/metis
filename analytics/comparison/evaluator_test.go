package comparison_test

import (
	"encoding/json"
	"testing"

	"github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

func TestEvaluateAlignsCompositePopulationWithoutGuessingZero(t *testing.T) {
	descriptor := comparisonDescriptor(
		[]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}},
		[]comparison.OutputRef{{Public: "dimension:sales.orders.channel", Column: "orders.channel"}, {Public: "dimension:sales.orders.region", Column: "orders.region"}},
	)
	schema := comparisonSchema(
		artifact.OutputColumn{Name: "orders.channel", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		artifact.OutputColumn{Name: "orders.region", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	)
	result, err := comparison.Evaluate(descriptor,
		comparison.Evidence{Schema: schema, Rows: [][]any{{"web", "north", "100"}, {"store", "south", "50"}}},
		comparison.Evidence{Schema: schema, Rows: [][]any{{"store", "east", "80"}, {"web", "north", "120"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 3 || string(result.Rows[0].Members[1].Value) != `"east"` || result.Rows[0].BaselinePresent || !result.Rows[0].CurrentPresent || result.Rows[0].Values[0].BaselineValue != nil {
		t.Fatalf("aligned result = %#v", result.Rows)
	}
	continuing := result.Rows[2]
	if got := string(*continuing.Values[0].Delta); got != "20" || string(*continuing.Values[0].PercentChange) != "20" {
		t.Fatalf("continuing row = %#v", continuing)
	}
}

func TestEvaluatePreservesNullAndZeroBaselineState(t *testing.T) {
	descriptor := comparisonDescriptor(
		[]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}},
		[]comparison.OutputRef{{Public: "dimension:sales.orders.segment", Column: "orders.segment"}},
	)
	schema := comparisonSchema(
		artifact.OutputColumn{Name: "orders.segment", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	)
	result, err := comparison.Evaluate(descriptor,
		comparison.Evidence{Schema: schema, Rows: [][]any{{"A", "0"}, {"B", nil}}},
		comparison.Evidence{Schema: schema, Rows: [][]any{{"A", "10"}, {"B", "5"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	zero := result.Rows[0].Values[0]
	if !zero.ChangeDefined || zero.PercentDefined || zero.PercentChange != nil || string(*zero.Delta) != "10" {
		t.Fatalf("zero baseline = %#v", zero)
	}
	null := result.Rows[1].Values[0]
	if null.ChangeDefined || null.Delta != nil || null.BaselineValue != nil || string(*null.CurrentValue) != "5" {
		t.Fatalf("null baseline = %#v", null)
	}
}

func TestEvaluateScalarMultipleMetricsAndSignedPercent(t *testing.T) {
	descriptor := comparisonDescriptor([]comparison.OutputRef{
		{Public: "metric:sales.orders", Column: "orders"},
		{Public: "metric:sales.revenue", Column: "revenue"},
	}, nil)
	schema := comparisonSchema(
		artifact.OutputColumn{Name: "orders", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeInteger},
		artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	)
	result, err := comparison.Evaluate(descriptor,
		comparison.Evidence{Schema: schema, Rows: [][]any{{json.Number("2"), "-10"}}},
		comparison.Evidence{Schema: schema, Rows: [][]any{{json.Number("3"), "-5"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0].Members) != 0 || string(*result.Rows[0].Values[0].PercentChange) != "50" || string(*result.Rows[0].Values[1].PercentChange) != "-50" {
		t.Fatalf("scalar result = %#v", result.Rows)
	}
}

func TestEvaluatePreservesDecimalPrecisionBeyondPercentScale(t *testing.T) {
	descriptor := comparisonDescriptor([]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}}, nil)
	schema := comparisonSchema(artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal})
	result, err := comparison.Evaluate(descriptor,
		comparison.Evidence{Schema: schema, Rows: [][]any{{"1.00000000000000000001"}}},
		comparison.Evidence{Schema: schema, Rows: [][]any{{"1.00000000000000000003"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := result.Rows[0].Values[0]
	if string(*value.BaselineValue) != "1.00000000000000000001" || string(*value.CurrentValue) != "1.00000000000000000003" || string(*value.Delta) != "0.00000000000000000002" {
		t.Fatalf("precision was lost: %#v", value)
	}
}

func TestEvaluateRejectsDuplicateNumericTupleAndUnsupportedMetric(t *testing.T) {
	descriptor := comparisonDescriptor(
		[]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}},
		[]comparison.OutputRef{{Public: "dimension:sales.orders.amount", Column: "orders.amount"}},
	)
	schema := comparisonSchema(
		artifact.OutputColumn{Name: "orders.amount", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeDecimal},
		artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	)
	if _, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema, Rows: [][]any{{"1.0", "2"}, {"1.00", "3"}}}, comparison.Evidence{Schema: schema}); err == nil {
		t.Fatal("numerically duplicate tuple unexpectedly evaluated")
	}
	schema.Columns[1].Datatype = ossie.DataTypeFloat
	if err := comparison.ValidateEvidenceSchema(descriptor, schema); err == nil {
		t.Fatal("float metric unexpectedly accepted")
	}
}

func TestEvaluateRejectsSchemaAndNormalizedTypeMismatch(t *testing.T) {
	descriptor := comparisonDescriptor([]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}}, nil)
	schema := comparisonSchema(artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal})
	if _, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema, Rows: [][]any{{float64(1)}}}, comparison.Evidence{Schema: schema}); err == nil {
		t.Fatal("binary float decimal evidence unexpectedly accepted")
	}
	other := schema
	other.Columns = append([]artifact.OutputColumn(nil), schema.Columns...)
	other.Columns[0].Name = "other"
	if _, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema}, comparison.Evidence{Schema: other}); err == nil {
		t.Fatal("divergent period schema unexpectedly accepted")
	}
}

func TestEvaluateMemberTypesAndOrderingAreDeterministic(t *testing.T) {
	tests := []struct {
		name     string
		datatype ossie.DataType
		rows     [][]any
		want     []string
	}{
		{name: "string and null", datatype: ossie.DataTypeString, rows: [][]any{{nil, "1"}, {"b", "1"}, {"a", "1"}}, want: []string{`"a"`, `"b"`, `null`}},
		{name: "integer numeric", datatype: ossie.DataTypeInteger, rows: [][]any{{json.Number("10"), "1"}, {json.Number("2"), "1"}, {json.Number("-1"), "1"}}, want: []string{`-1`, `2`, `10`}},
		{name: "decimal numeric", datatype: ossie.DataTypeDecimal, rows: [][]any{{"10.0", "1"}, {"2.00", "1"}, {"-1", "1"}}, want: []string{`"-1"`, `"2.00"`, `"10.0"`}},
		{name: "float numeric", datatype: ossie.DataTypeFloat, rows: [][]any{{json.Number("1e2"), "1"}, {json.Number("10.5"), "1"}, {json.Number("2.5"), "1"}}, want: []string{`2.5`, `10.5`, `1e2`}},
		{name: "boolean", datatype: ossie.DataTypeBoolean, rows: [][]any{{true, "1"}, {false, "1"}}, want: []string{`false`, `true`}},
		{name: "date", datatype: ossie.DataTypeDate, rows: [][]any{{"2026-02-01", "1"}, {"2026-01-01", "1"}}, want: []string{`"2026-01-01"`, `"2026-02-01"`}},
		{name: "time", datatype: ossie.DataTypeTime, rows: [][]any{{"12:00:00", "1"}, {"08:00:00", "1"}}, want: []string{`"08:00:00"`, `"12:00:00"`}},
		{name: "datetime", datatype: ossie.DataTypeDateTime, rows: [][]any{{"2026-02-01T00:00:00", "1"}, {"2026-01-01T00:00:00", "1"}}, want: []string{`"2026-01-01T00:00:00"`, `"2026-02-01T00:00:00"`}},
		{name: "datetime tz", datatype: ossie.DataTypeDateTimeTz, rows: [][]any{{"2026-02-01T00:00:00Z", "1"}, {"2026-01-01T00:00:00Z", "1"}}, want: []string{`"2026-01-01T00:00:00Z"`, `"2026-02-01T00:00:00Z"`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor := comparisonDescriptor([]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}}, []comparison.OutputRef{{Public: "dimension:sales.orders.member", Column: "orders.member"}})
			schema := comparisonSchema(
				artifact.OutputColumn{Name: "orders.member", Kind: artifact.OutputDimension, Datatype: test.datatype},
				artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
			)
			result, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema, Rows: test.rows}, comparison.Evidence{Schema: schema})
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(result.Rows))
			for i := range result.Rows {
				got[i] = string(result.Rows[i].Members[0].Value)
			}
			if len(got) != len(test.want) {
				t.Fatalf("members = %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("members = %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestEvaluateEmptyAndOneSidedScalarPopulations(t *testing.T) {
	descriptor := comparisonDescriptor([]comparison.OutputRef{{Public: "metric:sales.revenue", Column: "revenue"}}, nil)
	schema := comparisonSchema(artifact.OutputColumn{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal})
	empty, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema}, comparison.Evidence{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Rows == nil || len(empty.Rows) != 0 || empty.Dimensions == nil {
		t.Fatalf("empty result must use empty arrays: %#v", empty)
	}
	oneSided, err := comparison.Evaluate(descriptor, comparison.Evidence{Schema: schema}, comparison.Evidence{Schema: schema, Rows: [][]any{{"5"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(oneSided.Rows) != 1 || oneSided.Rows[0].BaselinePresent || !oneSided.Rows[0].CurrentPresent || oneSided.Rows[0].Values[0].BaselineValue != nil || oneSided.Rows[0].Values[0].ChangeDefined {
		t.Fatalf("one-sided scalar = %#v", oneSided.Rows)
	}
	encoded, err := json.Marshal(oneSided)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("JSON = %s, %v", encoded, err)
	}
}

func comparisonDescriptor(metrics, dimensions []comparison.OutputRef) comparison.Descriptor {
	return comparison.Descriptor{
		AnalysisID: "analysis", TimeDimension: "dimension:sales.orders.event_time",
		Baseline: comparison.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
		Current:  comparison.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
		Metrics:  metrics, Dimensions: dimensions,
	}
}

func comparisonSchema(columns ...artifact.OutputColumn) artifact.OutputSchema {
	return artifact.OutputSchema{Columns: columns}
}
