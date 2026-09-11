package conversion_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestBuildOutputSchemaPreservesProjectionOrderAndSemanticTypes(t *testing.T) {
	month := query.TimeGrainMonth
	plan := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{
		{Name: "order_date", Kind: semanticplan.ProjectionDimension, Field: &ossie.Field{Name: "order_date", Datatype: ossie.DataTypeDate}, Grain: &month},
		{Name: "region", Kind: semanticplan.ProjectionDimension, Field: &ossie.Field{Name: "region", Datatype: ossie.DataTypeString}},
		{Name: "revenue", Kind: semanticplan.ProjectionMetric, Metric: &ossie.Metric{Name: "revenue", Datatype: ossie.DataTypeDecimal}},
	}}

	got, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: "order_date", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeDate, Grain: &month},
		{Name: "region", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %#v, want %#v", got, want)
	}
	if got.Columns[0].Grain == plan.Projections[0].Grain {
		t.Fatal("output schema retained the plan's mutable grain pointer")
	}
}

func TestBuildOutputSchemaRejectsIncompleteProjection(t *testing.T) {
	_, err := conversion.BuildOutputSchema(&semanticplan.SemanticPlan{Projections: []semanticplan.Projection{{Name: "revenue", Kind: semanticplan.ProjectionMetric}}})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrInternalInvariant)
	}
}

func TestBuildOutputSchemaUsesCustomCalendarGrainWithoutChangingSQLProjection(t *testing.T) {
	fiscalWeek := query.TimeGrain("fiscal_week")
	plan := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{{
		Name:  "calendar_day",
		Kind:  semanticplan.ProjectionDimension,
		Field: &ossie.Field{Name: "fiscal_week_start", Datatype: ossie.DataTypeDate},
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain: fiscalWeek,
		},
	}}}

	schema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Projections[0].Grain != nil {
		t.Fatal("schema construction changed the physical projection grain")
	}
	if got := schema.Columns[0].Grain; got == nil || *got != fiscalWeek {
		t.Fatalf("custom calendar grain = %v, want %s", got, fiscalWeek)
	}
}

func TestBuildOutputSchemaPreservesUnspecifiedOssieDatatype(t *testing.T) {
	plan := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{{
		Name:   "untyped_metric",
		Kind:   semanticplan.ProjectionMetric,
		Metric: &ossie.Metric{Name: "untyped_metric"},
	}}}

	schema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.Columns[0].Datatype; got != "" {
		t.Fatalf("datatype = %q, want unspecified", got)
	}
}
