package ossie

import "testing"

func TestConversionMetricContract(t *testing.T) {
	isTime := true
	doc := &Document{
		Version: SupportedSpecVersion,
		SemanticModel: []SemanticModel{{
			Name: "commerce",
			Datasets: []Dataset{{
				Name:   "events",
				Source: "analytics.events",
				Fields: []Field{
					{Name: "base_time", Datatype: DataTypeDateTime, Dimension: &Dimension{IsTime: &isTime}, Expression: conversionTestExpression("base_time")},
					{Name: "conversion_time", Datatype: DataTypeDateTime, Dimension: &Dimension{IsTime: &isTime}, Expression: conversionTestExpression("conversion_time")},
					{Name: "user_id", Datatype: DataTypeString, Expression: conversionTestExpression("user_id")},
					{Name: "converted_user_id", Datatype: DataTypeString, Expression: conversionTestExpression("converted_user_id")},
					{Name: "session_id", Datatype: DataTypeString, Expression: conversionTestExpression("session_id")},
					{Name: "converted_session_id", Datatype: DataTypeString, Expression: conversionTestExpression("converted_session_id")},
				},
			}},
			Metrics: []Metric{
				{
					Name:             "signup_events",
					Expression:       conversionTestExpression("COUNT(*)"),
					CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_binding","time_dimension":"base_time"}`}},
				},
				{
					Name:             "purchase_events",
					Expression:       conversionTestExpression("COUNT(*)"),
					CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_binding","time_dimension":"conversion_time"}`}},
				},
				{
					Name:             "signup_to_purchase_rate",
					Expression:       conversionTestExpression("signup_to_purchase_rate"),
					CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"conversion","base_metric":"signup_events","conversion_metric":"purchase_events","entity":{"base_property":"user_id","conversion_property":"converted_user_id"},"calculation":"conversion_rate","window":{"count":7,"unit":"day"},"constant_properties":[{"base_property":"session_id","conversion_property":"converted_session_id"}]}`}},
				},
			},
		}},
	}
	if err := ValidateDocument(doc); err != nil {
		t.Fatal(err)
	}

	spec, ok, err := ConversionSpec(&doc.SemanticModel[0].Metrics[2])
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("conversion extension not found")
	}
	if spec.BaseMetric != "signup_events" || spec.ConversionMetric != "purchase_events" || spec.Calculation != ConversionCalculationConversionRate {
		t.Fatalf("spec = %#v", spec)
	}
	if spec.Window == nil || spec.Window.Count != 7 || spec.Window.Unit != "day" {
		t.Fatalf("window = %#v", spec.Window)
	}
}

func TestConversionMetricRejectsInvalidContract(t *testing.T) {
	base := ConversionMetricSpec{
		Kind:             MetricExtensionConversion,
		BaseMetric:       "signup_events",
		ConversionMetric: "purchase_events",
		Entity:           ConversionPropertyPair{BaseProperty: "user_id", ConversionProperty: "user_id"},
		Calculation:      ConversionCalculationConversions,
	}
	cases := map[string]ConversionMetricSpec{
		"same inputs":         func() ConversionMetricSpec { s := base; s.ConversionMetric = s.BaseMetric; return s }(),
		"missing entity":      func() ConversionMetricSpec { s := base; s.Entity = ConversionPropertyPair{}; return s }(),
		"unknown calculation": func() ConversionMetricSpec { s := base; s.Calculation = "funnel_score"; return s }(),
		"zero window":         func() ConversionMetricSpec { s := base; s.Window = &ConversionWindow{Count: 0, Unit: "day"}; return s }(),
		"unknown window unit": func() ConversionMetricSpec {
			s := base
			s.Window = &ConversionWindow{Count: 1, Unit: "fiscal_week"}
			return s
		}(),
		"duplicate entity constant": func() ConversionMetricSpec {
			s := base
			s.ConstantProperties = []ConversionPropertyPair{s.Entity}
			return s
		}(),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateConversionSpec(spec); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestConversionMetricRequiresInputTimeBindings(t *testing.T) {
	isTime := true
	model := &SemanticModel{
		Name: "commerce",
		Datasets: []Dataset{{
			Name: "events", Source: "analytics.events",
			Fields: []Field{
				{Name: "event_time", Datatype: DataTypeDateTime, Dimension: &Dimension{IsTime: &isTime}, Expression: conversionTestExpression("event_time")},
				{Name: "user_id", Datatype: DataTypeString, Expression: conversionTestExpression("user_id")},
			},
		}},
		Metrics: []Metric{
			{Name: "base", Expression: conversionTestExpression("COUNT(*)")},
			{Name: "converted", Expression: conversionTestExpression("COUNT(*)"), CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"time_binding","time_dimension":"event_time"}`}}},
		},
	}
	spec := ConversionMetricSpec{Kind: MetricExtensionConversion, BaseMetric: "base", ConversionMetric: "converted", Entity: ConversionPropertyPair{BaseProperty: "user_id", ConversionProperty: "user_id"}, Calculation: ConversionCalculationConversions}
	if err := ValidateConversionModel(model, "conversion", spec); err == nil {
		t.Fatal("expected missing base time_binding error")
	}
}

func conversionTestExpression(sql string) Expression {
	return Expression{Dialects: []DialectExpression{{Dialect: DialectANSISQL, Expression: sql}}}
}
