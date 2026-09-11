package builder

import (
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func attributionBundlePlanForTest(t *testing.T, request attribution.ResolvedMetricAttributionRequest, dimension string) *semanticplan.SemanticPlan {
	t.Helper()
	field := &ossie.Field{Name: dimension, Datatype: ossie.DataTypeString}
	metric := &ossie.Metric{
		Name:     request.MetricRef,
		Datatype: ossie.DataTypeDecimal,
		CustomExtensions: []ossie.CustomExtension{{
			VendorName: ossie.MetisExtensionVendor,
			Data:       `{"kind":"fill","policy":"zero"}`,
		}},
	}
	group := semanticplan.GroupBy{
		Name:       dimension,
		Dataset:    "orders",
		Field:      field,
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "orders." + dimension},
	}
	attribution, err := attribution.BuildAdditiveAttributionNode(
		request.MetricRef+"__attribution__"+dimension,
		request,
		additiveAttributionPlanForTest(),
		semanticplan.SemanticPlanNodeInput{NodeID: request.MetricRef, Grain: []semanticplan.GroupBy{group}},
		group,
		additiveAttributionTimeDimensionForTest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ownedPredicates := make([]semanticplan.SemanticPlanNodePredicate, 0, len(request.Filters))
	for i := range request.Filters {
		predicate := request.Filters[i]
		ownedPredicates = append(ownedPredicates, semanticplan.SemanticPlanNodePredicate{
			Scope:       semanticplan.SemanticPredicatePreAggregation,
			OwnerNodeID: request.MetricRef,
			Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
			Predicate:   &predicate,
		})
	}
	producer := semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        request.MetricRef,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
			Dimensions:                []string{dimension},
			OutputGrain:               []semanticplan.GroupBy{group},
			Predicates:                ownedPredicates,
		},
		Source: semanticplan.SemanticSourceState{
			SourceRoots:      []string{"orders"},
			RequiredDatasets: []string{"orders"},
			Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{request.MetricRef}},
		Metric:      metric,
		Expression:  expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "SUM(orders.revenue)"},
	}
	return &semanticplan.SemanticPlan{
		Model:      semanticplan.ModelRef{Project: request.ProjectID, Name: "sales"},
		Root:       semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Predicates: append([]semanticplan.Predicate(nil), request.Filters...),
		Groups:     []semanticplan.GroupBy{group},
		Requested:  []string{request.MetricRef},
		Nodes:      []semanticplan.SemanticPlanNode{producer, attribution},
		Output:     semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
	}
}

func TestMetricAttributionBundleKeepsIndependentPlansInCanonicalOrder(t *testing.T) {
	request := additiveAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	country := attributionBundlePlanForTest(t, request, "country")
	channel := attributionBundlePlanForTest(t, request, "channel")

	bundle, err := attribution.BuildMetricAttributionBundle(request, []attribution.MetricAttributionDimensionPlan{
		{Dimension: "country", Plan: country},
		{Dimension: "channel", Plan: channel},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.Request.Dimensions; len(got) != 2 || got[0] != "channel" || got[1] != "country" {
		t.Fatalf("canonical request dimensions = %#v", got)
	}
	if bundle.Queries[0].Dimension != "channel" || bundle.Queries[1].Dimension != "country" {
		t.Fatalf("bundle query order = %#v", bundle.Queries)
	}
	if bundle.Queries[0].Plan == channel || bundle.Queries[1].Plan == country {
		t.Fatal("bundle retained caller-owned semantic plan pointers")
	}
	for _, query := range bundle.Queries {
		attribution, ok := query.Plan.Nodes[1].(semanticplan.AdditiveAttributionNode)
		if !ok {
			t.Fatalf("bundle attribution node = %T", query.Plan.Nodes[1])
		}
		if attribution.DimensionRef != query.Dimension || len(attribution.Base.OutputGrain) != 1 {
			t.Fatalf("bundle combined independent dimensions: %#v", attribution)
		}
	}
}

func TestMetricAttributionBundleLowersEachDimensionToIndependentSQLPlan(t *testing.T) {
	request := additiveAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	bundle, err := attribution.BuildMetricAttributionBundle(request, []attribution.MetricAttributionDimensionPlan{
		{Dimension: "country", Plan: attributionBundlePlanForTest(t, request, "country")},
		{Dimension: "channel", Plan: attributionBundlePlanForTest(t, request, "channel")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range bundle.Queries {
		physical, lowerErr := conversion.BuildSQLPlan(query.Plan, mustRenderer(t, "DUCKDB"))
		if lowerErr != nil {
			t.Fatalf("lower dimension %q: %v", query.Dimension, lowerErr)
		}
		var rootFound bool
		for _, block := range physical.Blocks {
			if block.ID != physical.Root {
				continue
			}
			rootFound = true
			if len(block.Projections) != 6 || block.Projections[0].Alias != query.Dimension {
				t.Fatalf("dimension %q root projections = %#v", query.Dimension, block.Projections)
			}
		}
		if !rootFound {
			t.Fatalf("dimension %q SQLPlan has no root block", query.Dimension)
		}
	}
}

func TestMetricAttributionBundlePreservesSharedFiltersInEveryPeriodQuery(t *testing.T) {
	request := additiveAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	statusField := &ossie.Field{Name: "status", Datatype: ossie.DataTypeString}
	request.Filters = []semanticplan.Predicate{{
		Filter:     query.Filter{Field: "status", Operator: query.FilterEQ, Value: "completed"},
		Dataset:    "orders",
		Field:      statusField,
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "orders.status"},
	}}
	bundle, err := attribution.BuildMetricAttributionBundle(request, []attribution.MetricAttributionDimensionPlan{
		{Dimension: "country", Plan: attributionBundlePlanForTest(t, request, "country")},
		{Dimension: "channel", Plan: attributionBundlePlanForTest(t, request, "channel")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, bundled := range bundle.Queries {
		physical, lowerErr := conversion.BuildSQLPlan(bundled.Plan, mustRenderer(t, "DUCKDB"))
		if lowerErr != nil {
			t.Fatal(lowerErr)
		}
		for _, block := range physical.Blocks {
			if block.ID != additiveAttributionBaselineBlockID && block.ID != additiveAttributionCurrentBlockID {
				continue
			}
			if len(block.Predicates) != 3 {
				t.Fatalf("dimension %q period %q predicates = %#v", bundled.Dimension, block.ID, block.Predicates)
			}
		}
	}
}

func TestMetricAttributionBundleRejectsDivergentSharedSemantics(t *testing.T) {
	request := additiveAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	country := attributionBundlePlanForTest(t, request, "country")
	channel := attributionBundlePlanForTest(t, request, "channel")
	node := channel.Nodes[1].(semanticplan.AdditiveAttributionNode)
	node.Current.End = node.Current.End.AddDate(0, 1, 0)
	channel.Nodes[1] = node

	if _, err := attribution.BuildMetricAttributionBundle(request, []attribution.MetricAttributionDimensionPlan{
		{Dimension: "country", Plan: country},
		{Dimension: "channel", Plan: channel},
	}); err == nil {
		t.Fatal("bundle with divergent period semantics unexpectedly validated")
	}
}

func TestAdditiveAttributionOutputSchemaMatchesPhysicalProjection(t *testing.T) {
	request := additiveAttributionRequestForTest()
	plan := attributionBundlePlanForTest(t, request, "country")
	schema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"country", "baseline_value", "current_value", "delta", "total_delta", "contribution_pct"}
	if len(schema.Columns) != len(want) {
		t.Fatalf("output columns = %#v", schema.Columns)
	}
	for i, name := range want {
		if schema.Columns[i].Name != name {
			t.Fatalf("output column %d = %q, want %q", i, schema.Columns[i].Name, name)
		}
	}
	if schema.Columns[5].Datatype != ossie.DataTypeDecimal {
		t.Fatalf("contribution datatype = %q", schema.Columns[5].Datatype)
	}
}
