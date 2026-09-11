package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
)

func TestPlanConversionEventValueResolvesSumField(t *testing.T) {
	model := conversionPlannerModel(t)
	metric := resolver.ResolvedMetric{Name: "signup_events", Metric: &ossie.Metric{Name: "signup_events"}, Expression: resolvedTestExpression("SUM(signups.signup_value)")}
	plan, err := planConversionEventValue(resolvedTestQuery(model, "DUCKDB"), metric, "signups")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Kind != semanticplan.ConversionEventValueSumField || plan.Dataset != "signups" || plan.Field == nil {
		t.Fatalf("value plan = %#v", plan)
	}
	if plan.Field.Dataset != "signups" || plan.Field.Name != "signup_value" || plan.Field.Expression != "signup_value" {
		t.Fatalf("value field = %#v", plan.Field)
	}
}

func TestPlanConversionEventValueAcceptsCountRows(t *testing.T) {
	model := conversionPlannerModel(t)
	metric := resolver.ResolvedMetric{Name: "signup_events", Metric: &ossie.Metric{Name: "signup_events"}, Expression: resolvedTestExpression("COUNT(*)")}
	plan, err := planConversionEventValue(resolvedTestQuery(model, "DUCKDB"), metric, "signups")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Kind != semanticplan.ConversionEventValueCountRows || plan.Field != nil {
		t.Fatalf("count value plan = %#v", plan)
	}
}

func TestPlanConversionEventValueRejectsNonComposableAggregate(t *testing.T) {
	model := conversionPlannerModel(t)
	metric := resolver.ResolvedMetric{Name: "signup_events", Metric: &ossie.Metric{Name: "signup_events"}, Expression: resolvedTestExpression("AVG(signups.signup_value)")}
	if _, err := planConversionEventValue(resolvedTestQuery(model, "DUCKDB"), metric, "signups"); err == nil {
		t.Fatal("expected AVG conversion event input to be rejected")
	}
}

func TestPlanConversionEventValueRejectsCrossRootField(t *testing.T) {
	model := conversionPlannerModel(t)
	metric := resolver.ResolvedMetric{Name: "signup_events", Metric: &ossie.Metric{Name: "signup_events"}, Expression: resolvedTestExpression("SUM(purchases.purchase_value)")}
	if _, err := planConversionEventValue(resolvedTestQuery(model, "DUCKDB"), metric, "signups"); err == nil {
		t.Fatal("expected cross-root event value to be rejected")
	}
}
