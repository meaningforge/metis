package ossie

import "testing"

func TestCustomCalendarHierarchyValidation(t *testing.T) {
	spec := CustomCalendarSpec{
		Kind:     ModelExtensionCustomCalendar,
		Dataset:  "calendar",
		BaseTime: "date",
		Grains: []CustomCalendarGrain{
			{Name: "fiscal_week", BucketDimension: "fiscal_week_start", OrdinalDimension: "fiscal_week_ordinal", DenseMapping: true, ParentGrain: "fiscal_quarter"},
			{Name: "fiscal_quarter", BucketDimension: "fiscal_quarter_start", OrdinalDimension: "fiscal_quarter_ordinal", DenseMapping: true, ParentGrain: "fiscal_year"},
			{Name: "fiscal_year", BucketDimension: "fiscal_year_start", OrdinalDimension: "fiscal_year_ordinal", DenseMapping: true},
		},
	}
	if err := ValidateCustomCalendarSpec(spec); err != nil {
		t.Fatal(err)
	}
	if !CustomCalendarGrainIsDescendant(spec, "fiscal_week", "fiscal_year") {
		t.Fatal("fiscal_week should descend from fiscal_year")
	}
	if CustomCalendarGrainIsDescendant(spec, "fiscal_year", "fiscal_week") {
		t.Fatal("fiscal_year must not descend from fiscal_week")
	}

	missingParent := spec
	missingParent.Grains = append([]CustomCalendarGrain(nil), spec.Grains...)
	missingParent.Grains[0].ParentGrain = "missing"
	if err := ValidateCustomCalendarSpec(missingParent); err == nil {
		t.Fatal("expected missing parent validation error")
	}

	cycle := spec
	cycle.Grains = append([]CustomCalendarGrain(nil), spec.Grains...)
	cycle.Grains[2].ParentGrain = "fiscal_week"
	if err := ValidateCustomCalendarSpec(cycle); err == nil {
		t.Fatal("expected hierarchy cycle validation error")
	}
}

func TestCustomGrainToDateModelValidation(t *testing.T) {
	isTime := true
	model := &SemanticModel{
		Name:             "commerce",
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"date","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_ordinal","dense_mapping":true,"parent_grain":"fiscal_year"},{"name":"fiscal_year","bucket_dimension":"fiscal_year_start","ordinal_dimension":"fiscal_year_ordinal","dense_mapping":true}]}`}},
		Datasets: []Dataset{{
			Name: "calendar", Source: "analytics.calendar", Fields: []Field{
				{Name: "date", Datatype: DataTypeDate, Expression: testExpression("calendar.date"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_week_start", Datatype: DataTypeDate, Expression: testExpression("calendar.fiscal_week_start"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_week_ordinal", Datatype: DataTypeInteger, Expression: testExpression("calendar.fiscal_week_ordinal"), Dimension: &Dimension{}},
				{Name: "fiscal_year_start", Datatype: DataTypeDate, Expression: testExpression("calendar.fiscal_year_start"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_year_ordinal", Datatype: DataTypeInteger, Expression: testExpression("calendar.fiscal_year_ordinal"), Dimension: &Dimension{}},
			},
		}},
	}

	valid := CumulativeMetricSpec{Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "date", Window: CumulativeWindow{Type: "grain_to_date", Unit: "fiscal_year"}}
	if err := ValidateCumulativeSpec(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCumulativeCalendarModel(model, valid); err != nil {
		t.Fatal(err)
	}

	unknown := valid
	unknown.Window.Unit = "fiscal_period"
	if err := ValidateCumulativeCalendarModel(model, unknown); err == nil {
		t.Fatal("expected unknown custom reset grain validation error")
	}

	wrongTime := valid
	wrongTime.TimeDimension = "other_date"
	if err := ValidateCumulativeCalendarModel(model, wrongTime); err == nil {
		t.Fatal("expected canonical base time validation error")
	}
}
