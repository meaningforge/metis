package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

const conversionEvaluationModel = `
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
      - name: signup_to_purchase_rate
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "purchase_events / signup_events"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"conversion","base_metric":"signup_events","conversion_metric":"purchase_events","entity":{"base_property":"user_id","conversion_property":"purchaser_id"},"calculation":"conversion_rate","window":{"count":7,"unit":"day"}}'
`

func TestSemanticPlanDAGUsesDedicatedConversionStage(t *testing.T) {
	resolved := resolveMetricEvaluation(t, conversionEvaluationModel, query.SemanticQuery{
		Model: "commerce", Metrics: []query.MetricRef{{Name: "signup_to_purchase_rate"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	graph := plan
	if got := semanticNodeFixtureNames(semanticPlanNodeFixturesForTest(graph)); !reflect.DeepEqual(got, []string{"purchase_events", "signup_events", "signup_to_purchase_rate"}) && !reflect.DeepEqual(got, []string{"signup_events", "purchase_events", "signup_to_purchase_rate"}) {
		t.Fatalf("evaluation order = %#v", got)
	}
	owner := semanticPlanNodeFixturesForTest(graph)[len(semanticPlanNodeFixturesForTest(graph))-1]
	conversion, ok := owner.Node.(semanticplan.ConversionNode)
	if !ok {
		t.Fatalf("conversion node = %T", owner.Node)
	}
	if conversion.Conversion == nil || conversion.PhysicalInputs == nil {
		t.Fatalf("conversion state = %#v", conversion)
	}
	if conversion.Spec.BaseMetric != "signup_events" || conversion.Spec.ConversionMetric != "purchase_events" {
		t.Fatalf("conversion spec = %#v", conversion.Spec)
	}
	if conversion.Conversion.BaseRoot != "signups" || conversion.Conversion.ConversionRoot != "purchases" {
		t.Fatalf("conversion roots = %#v", conversion.Conversion)
	}
	if len(conversion.Conversion.BaseEventKey) != 1 || conversion.Conversion.BaseEventKey[0].Name != "signup_id" || len(conversion.Conversion.ConversionEventKey) != 1 || conversion.Conversion.ConversionEventKey[0].Name != "purchase_id" {
		t.Fatalf("conversion event keys = %#v / %#v", conversion.Conversion.BaseEventKey, conversion.Conversion.ConversionEventKey)
	}
	if conversion.Conversion.Assignment != semanticplan.ConversionAssignmentNearestPrecedingBase {
		t.Fatalf("conversion assignment = %q", conversion.Conversion.Assignment)
	}
	physical := conversion.PhysicalInputs
	if physical.BaseDataset.Name != "signups" || physical.BaseDataset.Source != "analytics.signups" || physical.ConversionDataset.Name != "purchases" || physical.ConversionDataset.Source != "analytics.purchases" {
		t.Fatalf("conversion physical datasets = %#v / %#v", physical.BaseDataset, physical.ConversionDataset)
	}
	if len(physical.BaseEventKey) != 1 || physical.BaseEventKey[0].Expression != "signup_id" || len(physical.ConversionEventKey) != 1 || physical.ConversionEventKey[0].Expression != "purchase_id" {
		t.Fatalf("conversion physical event keys = %#v / %#v", physical.BaseEventKey, physical.ConversionEventKey)
	}
	if physical.BaseTime.Expression != "signup_time" || physical.ConversionTime.Expression != "purchase_time" || physical.Entity.Base.Expression != "user_id" || physical.Entity.Conversion.Expression != "purchaser_id" {
		t.Fatalf("conversion physical links = %#v", physical)
	}
	if !reflect.DeepEqual(owner.SourceRoots, []string{"purchases", "signups"}) {
		t.Fatalf("conversion source roots = %#v", owner.SourceRoots)
	}
}
