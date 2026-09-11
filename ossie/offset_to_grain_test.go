package ossie

import "testing"

func TestOffsetToGrainContract(t *testing.T) {
	isTime := true
	model := &SemanticModel{
		Name: "commerce",
		Datasets: []Dataset{{
			Name: "orders", Source: "analytics.orders",
			Fields: []Field{{Name: "order_date", Datatype: DataTypeDate, Expression: testExpression("orders.order_date"), Dimension: &Dimension{IsTime: &isTime}}},
		}},
		Metrics: []Metric{
			{Name: "revenue", Expression: testExpression("SUM(orders.amount)")},
			{Name: "revenue_at_start_of_year", Expression: testExpression("revenue"), CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"order_date","grain":"year"}`}}},
		},
	}
	spec, ok, err := OffsetToGrainSpec(&model.Metrics[1])
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("offset-to-grain extension not found")
	}
	if spec.BaseMetric != "revenue" || spec.TimeDimension != "order_date" || spec.Grain != "year" {
		t.Fatalf("spec = %#v", spec)
	}
	metrics := map[string]struct{}{"revenue": {}, "revenue_at_start_of_year": {}}
	if err := validateOffsetToGrainMetrics(model, metrics); err != nil {
		t.Fatal(err)
	}
}

func TestOffsetToGrainRejectsInvalidContract(t *testing.T) {
	cases := map[string]OffsetToGrainMetricSpec{
		"wrong kind":    {Kind: MetricExtensionTimeOffset, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "year"},
		"missing base":  {Kind: MetricExtensionOffsetToGrain, TimeDimension: "order_date", Grain: "year"},
		"missing time":  {Kind: MetricExtensionOffsetToGrain, BaseMetric: "revenue", Grain: "year"},
		"missing grain": {Kind: MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateOffsetToGrainSpec(spec); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestOffsetToGrainCustomCalendarRequiresCanonicalTimeDimension(t *testing.T) {
	isTime := true
	model := &SemanticModel{
		Name: "commerce",
		Datasets: []Dataset{
			{Name: "orders", Source: "analytics.orders", Fields: []Field{
				{Name: "order_date", Datatype: DataTypeDate, Expression: testExpression("orders.order_date"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "other_date", Datatype: DataTypeDate, Expression: testExpression("orders.other_date"), Dimension: &Dimension{IsTime: &isTime}},
			}},
			{Name: "calendar", Source: "analytics.calendar", Fields: []Field{
				{Name: "calendar_date", Datatype: DataTypeDate, Expression: testExpression("calendar.calendar_date"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_year", Datatype: DataTypeDate, Expression: testExpression("calendar.fiscal_year"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_year_ordinal", Datatype: DataTypeInteger, Expression: testExpression("calendar.fiscal_year_ordinal")},
			}},
		},
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"order_date","calendar_time_dimension":"calendar_date","grains":[{"name":"fiscal_year","bucket_dimension":"fiscal_year","ordinal_dimension":"fiscal_year_ordinal","dense_mapping":true}]}`}},
	}
	good := OffsetToGrainMetricSpec{Kind: MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "fiscal_year"}
	if err := ValidateOffsetToGrainCalendarModel(model, good); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.TimeDimension = "other_date"
	if err := ValidateOffsetToGrainCalendarModel(model, bad); err == nil {
		t.Fatal("expected canonical custom-calendar time-dimension error")
	}
}

func TestOffsetToGrainRejectsUnknownCustomGrain(t *testing.T) {
	model := &SemanticModel{Name: "commerce"}
	spec := OffsetToGrainMetricSpec{Kind: MetricExtensionOffsetToGrain, BaseMetric: "revenue", TimeDimension: "order_date", Grain: "fiscal_year"}
	if err := ValidateOffsetToGrainCalendarModel(model, spec); err == nil {
		t.Fatal("expected unsupported custom grain error")
	}
}
