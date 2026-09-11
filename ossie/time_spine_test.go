package ossie

import "testing"

func TestTimeSpineContract(t *testing.T) {
	isTime := true
	model := SemanticModel{
		Name:             "commerce",
		CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_spine","dataset":"calendar","time_dimension":"day","grains":["day","week","month","quarter","year"]}`}},
		Datasets: []Dataset{{
			Name: "calendar", Source: "analytics.calendar",
			Fields: []Field{{Name: "day", Datatype: DataTypeDate, Dimension: &Dimension{IsTime: &isTime}}},
		}},
	}
	spec, ok, err := TimeSpine(&model)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("time spine extension not found")
	}
	if spec.Dataset != "calendar" || spec.TimeDimension != "day" {
		t.Fatalf("spec = %#v", spec)
	}
	if len(spec.Grains) != 5 {
		t.Fatalf("grains = %#v", spec.Grains)
	}
	if err := ValidateTimeSpineModel(&model, spec); err != nil {
		t.Fatal(err)
	}
}

func TestTimeSpineRejectsInvalidReferences(t *testing.T) {
	isTime := true
	model := SemanticModel{Datasets: []Dataset{{Name: "calendar", Source: "analytics.calendar", Fields: []Field{{Name: "day", Datatype: DataTypeDate, Dimension: &Dimension{IsTime: &isTime}}}}}}
	for name, spec := range map[string]TimeSpineSpec{
		"missing dataset": {Kind: ModelExtensionTimeSpine, Dataset: "missing", TimeDimension: "day", Grains: []string{"day"}},
		"missing field":   {Kind: ModelExtensionTimeSpine, Dataset: "calendar", TimeDimension: "missing", Grains: []string{"day"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTimeSpineModel(&model, spec); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestTimeSpineRejectsUnsupportedGrain(t *testing.T) {
	err := ValidateTimeSpineSpec(TimeSpineSpec{Kind: ModelExtensionTimeSpine, Dataset: "calendar", TimeDimension: "day", Grains: []string{"minute"}})
	if err == nil {
		t.Fatal("expected unsupported grain error")
	}
}
