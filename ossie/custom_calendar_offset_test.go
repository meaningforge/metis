package ossie

import "testing"

func TestCustomCalendarTimeOffsetUnit(t *testing.T) {
	isTime := true
	model := SemanticModel{
		Name:             "fiscal",
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true}]}`}},
		Datasets: []Dataset{{
			Name: "calendar", Source: "analytics.calendar",
			Fields: []Field{
				{Name: "day", Datatype: DataTypeDate, Expression: testExpression("calendar.day"), Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_week_start", Datatype: DataTypeDate, Expression: testExpression("calendar.fiscal_week_start"), Dimension: &Dimension{}},
				{Name: "fiscal_week_index", Datatype: DataTypeInteger, Expression: testExpression("calendar.fiscal_week_index"), Dimension: &Dimension{}},
				{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("calendar.revenue")},
			},
		}},
		Metrics: []Metric{
			{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("SUM(calendar.revenue)")},
			{Name: "revenue_prior_fiscal_week", Datatype: DataTypeDecimal, Expression: testExpression("revenue"), CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_offset","base_metric":"revenue","time_dimension":"day","offset":{"count":-1,"unit":"fiscal_week"}}`}}},
		},
	}
	doc := &Document{Version: SupportedSpecVersion, SemanticModel: []SemanticModel{model}}
	if err := ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCalendarTimeOffsetRequiresDenseMappingAssertion(t *testing.T) {
	model := customCalendarOffsetModelWithDensity("fiscal_week", "day", false)
	if err := ValidateDocument(&Document{Version: SupportedSpecVersion, SemanticModel: []SemanticModel{model}}); err == nil {
		t.Fatal("expected custom offset without dense mapping assertion to fail")
	}
}

func TestCustomCalendarTimeOffsetRejectsUnknownUnit(t *testing.T) {
	model := customCalendarOffsetModel("retail_week", "day")
	if err := ValidateDocument(&Document{Version: SupportedSpecVersion, SemanticModel: []SemanticModel{model}}); err == nil {
		t.Fatal("expected unknown custom offset unit to fail")
	}
}

func TestCustomCalendarTimeOffsetRequiresCanonicalBaseTime(t *testing.T) {
	model := customCalendarOffsetModel("fiscal_week", "other_day")
	if err := ValidateDocument(&Document{Version: SupportedSpecVersion, SemanticModel: []SemanticModel{model}}); err == nil {
		t.Fatal("expected non-canonical custom offset time dimension to fail")
	}
}

func TestBuiltInTimeOffsetUnitDoesNotRequireCustomCalendar(t *testing.T) {
	isTime := true
	model := SemanticModel{
		Name: "sales",
		Datasets: []Dataset{{Name: "orders", Source: "orders", Fields: []Field{
			{Name: "day", Datatype: DataTypeDate, Expression: testExpression("orders.day"), Dimension: &Dimension{IsTime: &isTime}},
			{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("orders.revenue")},
		}}},
		Metrics: []Metric{
			{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("SUM(orders.revenue)")},
			{Name: "revenue_prior_week", Datatype: DataTypeDecimal, Expression: testExpression("revenue"), CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_offset","base_metric":"revenue","time_dimension":"day","offset":{"count":-1,"unit":"week"}}`}}},
		},
	}
	if err := ValidateDocument(&Document{Version: SupportedSpecVersion, SemanticModel: []SemanticModel{model}}); err != nil {
		t.Fatal(err)
	}
}

func customCalendarOffsetModel(unit, timeDimension string) SemanticModel {
	return customCalendarOffsetModelWithDensity(unit, timeDimension, true)
}

func customCalendarOffsetModelWithDensity(unit, timeDimension string, dense bool) SemanticModel {
	isTime := true
	denseJSON := "false"
	if dense {
		denseJSON = "true"
	}
	return SemanticModel{
		Name:             "fiscal",
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":` + denseJSON + `}]}`}},
		Datasets: []Dataset{{Name: "calendar", Source: "analytics.calendar", Fields: []Field{
			{Name: "day", Datatype: DataTypeDate, Expression: testExpression("calendar.day"), Dimension: &Dimension{IsTime: &isTime}},
			{Name: "other_day", Datatype: DataTypeDate, Expression: testExpression("calendar.other_day"), Dimension: &Dimension{IsTime: &isTime}},
			{Name: "fiscal_week_start", Datatype: DataTypeDate, Expression: testExpression("calendar.fiscal_week_start"), Dimension: &Dimension{}},
			{Name: "fiscal_week_index", Datatype: DataTypeInteger, Expression: testExpression("calendar.fiscal_week_index"), Dimension: &Dimension{}},
			{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("calendar.revenue")},
		}}},
		Metrics: []Metric{
			{Name: "revenue", Datatype: DataTypeDecimal, Expression: testExpression("SUM(calendar.revenue)")},
			{Name: "offset_revenue", Datatype: DataTypeDecimal, Expression: testExpression("revenue"), CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_offset","base_metric":"revenue","time_dimension":"` + timeDimension + `","offset":{"count":-1,"unit":"` + unit + `"}}`}}},
		},
	}
}
