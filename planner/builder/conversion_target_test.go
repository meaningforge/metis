package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
)

func TestPlanConversionPhysicalInputsUsesTargetSpecificExpressions(t *testing.T) {
	model := conversionPlannerModel(t)
	model.Fields["signups.user_id"].Field.Expression.Dialects = append(
		model.Fields["signups.user_id"].Field.Expression.Dialects,
		ossie.DialectExpression{Dialect: ossie.Dialect("CLICKHOUSE"), Expression: "toString(user_id)"},
	)
	q := resolvedTestQuery(model, "CLICKHOUSE")
	nodes := conversionPlannerResolvedMetricEvaluationNodes(t, q)
	semantic, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := planConversionPhysicalInputs(q, semantic, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if physical.BaseDataset.Source != "analytics.signups" || physical.ConversionDataset.Source != "analytics.purchases" {
		t.Fatalf("sources = %#v -> %#v", physical.BaseDataset, physical.ConversionDataset)
	}
	if physical.Entity.Base.Expression != "toString(user_id)" {
		t.Fatalf("target-specific entity expression = %q", physical.Entity.Base.Expression)
	}
	if physical.Entity.Conversion.Expression != "purchaser_id" {
		t.Fatalf("ANSI fallback expression = %q", physical.Entity.Conversion.Expression)
	}
	if physical.BaseTime.Expression != "signup_time" || physical.ConversionTime.Expression != "purchase_time" {
		t.Fatalf("time expressions = %#v -> %#v", physical.BaseTime, physical.ConversionTime)
	}
	if physical.BaseEventKey[0].Expression != "signup_id" || physical.ConversionEventKey[0].Expression != "purchase_id" {
		t.Fatalf("event key expressions = %#v -> %#v", physical.BaseEventKey, physical.ConversionEventKey)
	}
	if physical.BaseValue.Kind != semanticplan.ConversionEventValueSumField || physical.BaseValue.Field == nil || physical.BaseValue.Field.Expression != "signup_value" {
		t.Fatalf("base event value = %#v", physical.BaseValue)
	}
	if physical.ConversionValue.Kind != semanticplan.ConversionEventValueSumField || physical.ConversionValue.Field == nil || physical.ConversionValue.Field.Expression != "purchase_value" {
		t.Fatalf("conversion event value = %#v", physical.ConversionValue)
	}
}

func TestPlanConversionPhysicalInputsRetainsCountRowValues(t *testing.T) {
	model := conversionPlannerModel(t)
	model.Metrics["signup_events"].Expression.Dialects = []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "COUNT(*)"}}
	model.Metrics["purchase_events"].Expression.Dialects = []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "COUNT(*)"}}
	q := resolvedTestQuery(model, "DORIS")
	nodes := conversionPlannerResolvedMetricEvaluationNodes(t, q)
	semantic, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := planConversionPhysicalInputs(q, semantic, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if physical.BaseValue.Kind != semanticplan.ConversionEventValueCountRows || physical.BaseValue.Field != nil {
		t.Fatalf("base count-row value = %#v", physical.BaseValue)
	}
	if physical.ConversionValue.Kind != semanticplan.ConversionEventValueCountRows || physical.ConversionValue.Field != nil {
		t.Fatalf("conversion count-row value = %#v", physical.ConversionValue)
	}
}

func TestPlanConversionPhysicalInputsRejectsMissingTargetExpression(t *testing.T) {
	model := conversionPlannerModel(t)
	model.Fields["signups.user_id"].Field.Expression.Dialects = []ossie.DialectExpression{{Dialect: ossie.DialectSnowflake, Expression: "user_id"}}
	q := resolvedTestQuery(model, "CLICKHOUSE")
	nodes := conversionPlannerResolvedMetricEvaluationNodes(t, q)
	semantic, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planConversionPhysicalInputs(q, semantic, nodes); err == nil {
		t.Fatal("expected missing ClickHouse/ANSI conversion field expression to be rejected")
	}
}

func TestPlanConversionPhysicalInputsDoesNotReopenSemanticManifestMetricDefinition(t *testing.T) {
	model := conversionPlannerModel(t)
	q := resolvedTestQuery(model, "DORIS")
	nodes := conversionPlannerResolvedMetricEvaluationNodes(t, q)
	semantic, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), nodes)
	if err != nil {
		t.Fatal(err)
	}

	// The validated evaluation.MetricEvaluationNode owns the metric definition/expression
	// below the query-scoped metric-plan boundary. Physical conversion planning
	// must not reopen model.Metrics to obtain a competing answer.
	model.Metrics["signup_events"] = nil
	model.Metrics["purchase_events"] = nil
	physical, err := planConversionPhysicalInputs(q, semantic, nodes)
	if err != nil {
		t.Fatalf("physical conversion planning reopened manifest metric state: %v", err)
	}
	if physical.BaseValue.Kind != semanticplan.ConversionEventValueSumField || physical.ConversionValue.Kind != semanticplan.ConversionEventValueSumField {
		t.Fatalf("physical conversion values = %#v / %#v", physical.BaseValue, physical.ConversionValue)
	}
}

func conversionPlannerResolvedMetricEvaluationNodes(t *testing.T, q *resolver.ResolvedSemanticQuery) map[string]evaluation.MetricEvaluationNode {
	t.Helper()
	if q == nil || q.Model == nil {
		t.Fatal("resolved query/model is required")
	}
	nodes := conversionPlannerMetricEvaluationNodes()
	for name, node := range nodes {
		metric := q.Model.Metrics[name]
		if metric == nil {
			t.Fatalf("metric %q is unavailable", name)
		}
		selected, ok := q.MetricExpression(name)
		if !ok {
			t.Fatalf("resolved metric expression %q is unavailable", name)
		}
		node.Metric = semanticplan.CloneMetric(metric)
		node.Expression = semanticplan.CloneResolvedExpression(selected)
		nodes[name] = node
	}
	return nodes
}
