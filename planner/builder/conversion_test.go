package builder

import (
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestPlanConversionResolvesEventSides(t *testing.T) {
	model := conversionPlannerModel(t)
	spec := conversionPlannerSpec()
	plan, err := planConversion(model, "signup_to_purchase_rate", spec, conversionPlannerMetricEvaluationNodes())
	if err != nil {
		t.Fatal(err)
	}
	if plan.BaseRoot != "signups" || plan.ConversionRoot != "purchases" {
		t.Fatalf("roots = %s -> %s", plan.BaseRoot, plan.ConversionRoot)
	}
	if len(plan.BaseEventKey) != 1 || plan.BaseEventKey[0] != (semanticplan.ConversionFieldRef{Dataset: "signups", Name: "signup_id"}) {
		t.Fatalf("base event key = %#v", plan.BaseEventKey)
	}
	if len(plan.ConversionEventKey) != 1 || plan.ConversionEventKey[0] != (semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchase_id"}) {
		t.Fatalf("conversion event key = %#v", plan.ConversionEventKey)
	}
	if plan.Assignment != semanticplan.ConversionAssignmentNearestPrecedingBase {
		t.Fatalf("assignment = %q", plan.Assignment)
	}
	if plan.BaseTime != (semanticplan.ConversionFieldRef{Dataset: "signups", Name: "signup_time"}) {
		t.Fatalf("base time = %#v", plan.BaseTime)
	}
	if plan.ConversionTime != (semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchase_time"}) {
		t.Fatalf("conversion time = %#v", plan.ConversionTime)
	}
	if plan.Entity.Base.Dataset != "signups" || plan.Entity.Conversion.Dataset != "purchases" {
		t.Fatalf("entity = %#v", plan.Entity)
	}
	if len(plan.ConstantProperties) != 1 || plan.ConstantProperties[0].Base.Name != "session_id" || plan.ConstantProperties[0].Conversion.Name != "purchase_session_id" {
		t.Fatalf("constant properties = %#v", plan.ConstantProperties)
	}
	if plan.Window == nil || plan.Window.Count != 7 || plan.Window.Unit != "day" {
		t.Fatalf("window = %#v", plan.Window)
	}
}

func TestPlanConversionRejectsWrongSideProperty(t *testing.T) {
	model := conversionPlannerModel(t)
	spec := conversionPlannerSpec()
	spec.Entity.BaseProperty = "purchaser_id"
	if _, err := planConversion(model, "conversions", spec, conversionPlannerMetricEvaluationNodes()); err == nil {
		t.Fatal("expected wrong-side entity property to be rejected")
	}
}

func TestPlanConversionRejectsIncompatibleLinkTypes(t *testing.T) {
	model := conversionPlannerModel(t)
	model.Fields["purchases.purchaser_id"].Field.Datatype = ossie.DataTypeInteger
	if _, err := planConversion(model, "conversions", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes()); err == nil {
		t.Fatal("expected incompatible entity datatypes to be rejected")
	}
}

func TestPlanConversionRequiresStableEventIdentity(t *testing.T) {
	model := conversionPlannerModel(t)
	model.Datasets["signups"].PrimaryKey = nil
	if _, err := planConversion(model, "conversions", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes()); err == nil {
		t.Fatal("expected missing base event primary key to be rejected")
	}
}

func TestPlanConversionRejectsDerivedEventInputFromMetricEvaluationPlan(t *testing.T) {
	model := conversionPlannerModel(t)
	nodes := conversionPlannerMetricEvaluationNodes()
	node := nodes["signup_events"]
	node.Kind = evaluation.MetricEvaluationDerived
	node.Inputs = []evaluation.MetricEvaluationInput{{Metric: "purchase_events"}}
	node.Spec = evaluation.MetricEvaluationSpec{Derived: &evaluation.DerivedMetricEvaluationSpec{}}
	nodes["signup_events"] = node
	if _, err := planConversion(model, "conversions", conversionPlannerSpec(), nodes); err == nil {
		t.Fatal("expected derived conversion input to be rejected")
	}
}

func TestPlanConversionIgnoresSemanticManifestMetricEdgesForSourceInput(t *testing.T) {
	model := conversionPlannerModel(t)
	dependency, ok := model.MetricDependency("signup_events")
	if !ok {
		t.Fatal("signup_events dependency missing")
	}
	dependency.Metrics = []string{"purchase_events"}
	model.MetricDependencies["signup_events"] = dependency
	if _, err := planConversion(model, "conversions", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes()); err != nil {
		t.Fatalf("manifest metric edge must not override evaluation.MetricEvaluationPlan authority: %v", err)
	}
}

func TestPlanConversionRejectsMultiDatasetEventExpression(t *testing.T) {
	model := conversionPlannerModel(t)
	dependency, ok := model.MetricDependency("signup_events")
	if !ok {
		t.Fatal("signup_events dependency missing")
	}
	dependency.DirectDatasets = []string{"purchases", "signups"}
	model.MetricDependencies["signup_events"] = dependency
	if _, err := planConversion(model, "conversions", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes()); err == nil {
		t.Fatal("expected multi-dataset conversion input to be rejected")
	}
}

func conversionPlannerMetricEvaluationNodes() map[string]evaluation.MetricEvaluationNode {
	return map[string]evaluation.MetricEvaluationNode{
		"signup_events": {
			ID:   "signup_events",
			Kind: evaluation.MetricEvaluationSource,
			Spec: evaluation.MetricEvaluationSpec{Source: &evaluation.SourceMetricEvaluationSpec{}},
			TimeBinding: &ossie.MetricTimeBindingSpec{
				Kind:          ossie.MetricExtensionTimeBinding,
				TimeDimension: "signup_time",
			},
		},
		"purchase_events": {
			ID:   "purchase_events",
			Kind: evaluation.MetricEvaluationSource,
			Spec: evaluation.MetricEvaluationSpec{Source: &evaluation.SourceMetricEvaluationSpec{}},
			TimeBinding: &ossie.MetricTimeBindingSpec{
				Kind:          ossie.MetricExtensionTimeBinding,
				TimeDimension: "purchase_time",
			},
		},
	}
}

func conversionPlannerSpec() ossie.ConversionMetricSpec {
	return ossie.ConversionMetricSpec{
		Kind:       ossie.MetricExtensionConversion,
		BaseMetric: "signup_events", ConversionMetric: "purchase_events",
		Entity:             ossie.ConversionPropertyPair{BaseProperty: "user_id", ConversionProperty: "purchaser_id"},
		Calculation:        ossie.ConversionCalculationConversionRate,
		Window:             &ossie.ConversionWindow{Count: 7, Unit: "day"},
		ConstantProperties: []ossie.ConversionPropertyPair{{BaseProperty: "session_id", ConversionProperty: "purchase_session_id"}},
	}
}

func conversionPlannerModel(t *testing.T) *manifest.ModelIndex {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: commerce
    datasets:
      - name: signups
        source: analytics.signups
        primary_key: [signup_id]
        fields:
          - name: signup_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: signup_id}]
          - name: signup_time
            datatype: DateTime
            dimension: {is_time: true}
            expression:
              dialects: [{dialect: ANSI_SQL, expression: signup_time}]
          - name: user_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: user_id}]
          - name: session_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: session_id}]
          - name: signup_value
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: signup_value}]
      - name: purchases
        source: analytics.purchases
        primary_key: [purchase_id]
        fields:
          - name: purchase_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchase_id}]
          - name: purchase_time
            datatype: DateTime
            dimension: {is_time: true}
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchase_time}]
          - name: purchaser_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchaser_id}]
          - name: purchase_session_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchase_session_id}]
          - name: purchase_value
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchase_value}]
    metrics:
      - name: signup_events
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(signups.signup_value)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"signup_time"}'
      - name: purchase_events
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(purchases.purchase_value)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"purchase_time"}'
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, err := snapshot.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := project.Model("commerce")
	if err != nil {
		t.Fatal(err)
	}
	return model
}
