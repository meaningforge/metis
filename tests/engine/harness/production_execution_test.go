package harness

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestConformanceResultConsumesRunnerNormalizedValues(t *testing.T) {
	result := runner.ResultSet{
		Schema: artifact.OutputSchema{Columns: []artifact.OutputColumn{
			{Name: "region", Datatype: ossie.DataTypeString},
			{Name: "revenue", Datatype: ossie.DataTypeDecimal},
			{Name: "orders", Datatype: ossie.DataTypeInteger},
			{Name: "active", Datatype: ossie.DataTypeBoolean},
			{Name: "day", Datatype: ossie.DataTypeDate},
		}},
		Rows: [][]any{{"APAC", "1.250000000000000000", json.Number("2"), true, "2026-09-02"}, {"EU", nil, json.Number("0"), false, "2026-09-03"}},
	}
	got := conformanceResult(t, result)
	wantColumns := []scenarios.ResultColumn{
		{Name: "region", ValueKind: scenarios.ResultString},
		{Name: "revenue", ValueKind: scenarios.ResultNumber},
		{Name: "orders", ValueKind: scenarios.ResultInteger},
		{Name: "active", ValueKind: scenarios.ResultBoolean},
		{Name: "day", ValueKind: scenarios.ResultDate},
	}
	if !reflect.DeepEqual(got.Columns, wantColumns) {
		t.Fatalf("columns = %#v, want %#v", got.Columns, wantColumns)
	}
	if got.Rows[0][1].Canonical != "5/4" || !got.Rows[1][1].Null {
		t.Fatalf("normalized rows = %#v", got.Rows)
	}
}

func TestSelectOutputSchemaUsesRequestedProjectionOrder(t *testing.T) {
	schema := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: "segment", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		{Name: "segment_effect", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "attribution_defined", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeBoolean},
	}}
	got := SelectOutputSchema(t, schema, "attribution_defined", "segment_effect")
	want := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		schema.Columns[2], schema.Columns[1],
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected schema = %#v, want %#v", got, want)
	}
}
