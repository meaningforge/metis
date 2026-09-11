package ossie

import "testing"

func TestCustomCalendarContract(t *testing.T) {
	isTime := true
	model := SemanticModel{
		Name:             "commerce",
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index"}]}`}},
		Datasets: []Dataset{{
			Name: "calendar", Source: "analytics.calendar",
			Fields: []Field{
				{Name: "day", Datatype: DataTypeDate, Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_week_start", Datatype: DataTypeDate, Dimension: &Dimension{IsTime: &isTime}},
				{Name: "fiscal_week_index", Datatype: DataTypeInteger, Dimension: &Dimension{}},
			},
		}},
	}
	spec, ok, err := CustomCalendar(&model)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("custom calendar extension not found")
	}
	grain, ok := CustomCalendarGrainByName(spec, "fiscal_week")
	if !ok || grain.BucketDimension != "fiscal_week_start" || grain.OrdinalDimension != "fiscal_week_index" {
		t.Fatalf("grain = %#v", grain)
	}
	if err := ValidateCustomCalendarModel(&model, spec); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCalendarRejectsBuiltInAndDuplicateGrains(t *testing.T) {
	cases := []CustomCalendarSpec{
		{Kind: ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "day", Grains: []CustomCalendarGrain{{Name: "month", BucketDimension: "fiscal_month", OrdinalDimension: "fiscal_month_index"}}},
		{Kind: ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "day", Grains: []CustomCalendarGrain{{Name: "fiscal_week", BucketDimension: "a", OrdinalDimension: "i"}, {Name: "fiscal_week", BucketDimension: "b", OrdinalDimension: "j"}}},
	}
	for _, spec := range cases {
		if err := ValidateCustomCalendarSpec(spec); err == nil {
			t.Fatalf("expected validation error for %#v", spec)
		}
	}
}

func TestCustomCalendarRejectsInvalidFieldRoles(t *testing.T) {
	isTime := true
	model := SemanticModel{Datasets: []Dataset{{Name: "calendar", Source: "analytics.calendar", Fields: []Field{
		{Name: "day", Datatype: DataTypeDate, Dimension: &Dimension{IsTime: &isTime}},
		{Name: "bucket", Datatype: DataTypeString, Dimension: &Dimension{}},
		{Name: "ordinal", Datatype: DataTypeString, Dimension: &Dimension{}},
	}}}}
	spec := CustomCalendarSpec{Kind: ModelExtensionCustomCalendar, Dataset: "calendar", BaseTime: "day", Grains: []CustomCalendarGrain{{Name: "fiscal_week", BucketDimension: "bucket", OrdinalDimension: "ordinal"}}}
	if err := ValidateCustomCalendarModel(&model, spec); err == nil {
		t.Fatal("expected invalid custom calendar field roles")
	}
}
