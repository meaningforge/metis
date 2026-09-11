package compiler_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestSemiAdditiveMetricBuildsLatestSnapshotEvaluationStage(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	q := query.SemanticQuery{Project: projectName, Model: modelName, Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), q, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("nodes = %#v, want inventory_quantity + inventory_balance", plan.Nodes)
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok || base.NodeBase().ID != "inventory_quantity" {
		t.Fatalf("base node = %#v", plan.Nodes[0])
	}
	latest, ok := plan.Nodes[1].(semanticplan.SemiAdditiveNode)
	if !ok || latest.NodeBase().ID != "inventory_balance" || latest.Kind() != semanticplan.SemanticPlanNodeSemiAdditiveLast {
		t.Fatalf("semi-additive node = %#v", plan.Nodes[1])
	}
	latestBase := latest.NodeBase()
	if len(latestBase.Inputs) != 1 || latestBase.Inputs[0].NodeID != "inventory_quantity" {
		t.Fatalf("inputs = %#v", latestBase.Inputs)
	}
	spec := latest.Spec
	if spec.BaseMetric != "inventory_quantity" || spec.NonAdditiveDimension != "snapshot_date" || spec.Aggregation != "last" {
		t.Fatalf("semi-additive spec = %#v", spec)
	}
	if got := groupNames(base.NodeBase().OutputGrain); !reflect.DeepEqual(got, []string{"warehouse", "snapshot_date"}) {
		t.Fatalf("base input grain = %v, want warehouse + hidden snapshot_date", got)
	}
	if got := groupNames(latestBase.OutputGrain); !reflect.DeepEqual(got, []string{"warehouse"}) {
		t.Fatalf("semi-additive output grain = %v, want user-visible warehouse only", got)
	}
}

func TestSemiAdditiveQueriedTimeGrainPreservesRawOrderingKey(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	week := query.TimeGrainWeek
	q := query.SemanticQuery{Project: projectName, Model: modelName, Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "snapshot_date", Grain: &week}}}
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), q, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("nodes = %#v", plan.Nodes)
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok {
		t.Fatalf("base node = %#v", plan.Nodes[0])
	}
	latest, ok := plan.Nodes[1].(semanticplan.SemiAdditiveNode)
	if !ok {
		t.Fatalf("semi-additive node = %#v", plan.Nodes[1])
	}
	baseGrain := base.NodeBase().OutputGrain
	latestGrain := latest.NodeBase().OutputGrain
	if got := groupNames(baseGrain); !reflect.DeepEqual(got, []string{"snapshot_date", "__metis_ordered_snapshot_date"}) {
		t.Fatalf("base input grain = %v, want visible weekly bucket + hidden raw ordering key", got)
	}
	if baseGrain[0].Grain == nil || *baseGrain[0].Grain != query.TimeGrainWeek {
		t.Fatalf("visible snapshot grain = %#v, want week", baseGrain[0].Grain)
	}
	if baseGrain[1].Grain != nil {
		t.Fatalf("hidden raw ordering grain = %#v, want nil", baseGrain[1].Grain)
	}
	if got := groupNames(latestGrain); !reflect.DeepEqual(got, []string{"snapshot_date"}) {
		t.Fatalf("semi-additive output grain = %v, want visible weekly snapshot bucket", got)
	}
	if latestGrain[0].Grain == nil || *latestGrain[0].Grain != query.TimeGrainWeek {
		t.Fatalf("derived output grain = %#v, want week", latestGrain[0].Grain)
	}
}

func TestSemiAdditiveAsOfFiltersApplyBeforeLatestSelection(t *testing.T) {
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "inventory_balance"}},
		Dimensions: []query.DimensionRef{{Name: "warehouse"}},
		Filters: []query.Filter{
			{Field: "snapshot_date", Operator: query.FilterLTE, Value: "2026-06-30"},
			{Field: "warehouse", Operator: query.FilterEQ, Value: "tokyo"},
		},
	}
	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("nodes = %#v", plan.Nodes)
	}
	base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
	if !ok {
		t.Fatalf("base node = %#v", plan.Nodes[0])
	}
	baseNode := base.NodeBase()
	basePredicates := make([]semanticplan.Predicate, 0, len(baseNode.Predicates))
	for _, predicate := range baseNode.Predicates {
		if predicate.Scope == semanticplan.SemanticPredicatePreAggregation && predicate.Predicate != nil {
			basePredicates = append(basePredicates, *predicate.Predicate)
		}
	}
	if len(basePredicates) != 2 {
		t.Fatalf("base predicates = %#v, want as-of + warehouse filters", baseNode.Predicates)
	}
	seen := map[string]bool{}
	for _, predicate := range basePredicates {
		seen[predicate.Filter.Field] = true
	}
	if !seen["snapshot_date"] || !seen["warehouse"] {
		t.Fatalf("base predicates = %#v, want snapshot_date and warehouse", baseNode.Predicates)
	}
	if len(plan.Output.Predicates) != 0 {
		t.Fatalf("post predicates = %#v, as-of filter must apply before latest selection", plan.Output.Predicates)
	}
	wherePos := strings.Index(sqlQuery.SQL, "WHERE")
	latestPos := strings.Index(sqlQuery.SQL, "MAX_BY(")
	if wherePos < 0 || latestPos < 0 || wherePos > latestPos {
		t.Fatalf("as-of WHERE must appear before latest-value evaluation:\n%s", sqlQuery.SQL)
	}
	if len(sqlQuery.Parameters) != 2 {
		t.Fatalf("parameters = %d, want 2", len(sqlQuery.Parameters))
	}
}

func groupNames(groups []semanticplan.GroupBy) []string {
	out := make([]string, len(groups))
	for i := range groups {
		out[i] = groups[i].Name
	}
	return out
}
