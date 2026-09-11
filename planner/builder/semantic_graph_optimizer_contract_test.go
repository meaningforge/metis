package builder

import (
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

type legacyOnlyStagedRule struct {
	called *bool
}

func (legacyOnlyStagedRule) Name() string { return "legacy_only" }

func (rule legacyOnlyStagedRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	*rule.called = true
	if plan.DenseCalendar != nil {
		plan.DenseCalendar.QueryTimeDimension = "legacy_mutation"
	}
	return true, nil
}

type graphOnlyDispatchRule struct{}

func (graphOnlyDispatchRule) Name() string { return "graph_only_dispatch" }

func (graphOnlyDispatchRule) Apply(*semanticplan.SemanticPlan) (bool, error) {
	return false, errors.New("legacy Apply must not be called for staged optimization")
}

func (graphOnlyDispatchRule) ApplySemanticPlan(state *optimizer.State) (bool, error) {
	if state == nil || state.DenseCalendar == nil {
		return false, errors.New("canonical calendar state is required")
	}
	if state.DenseCalendar.QueryTimeDimension == "optimized_order_date" {
		return false, nil
	}
	state.DenseCalendar.QueryTimeDimension = "optimized_order_date"
	return true, nil
}

func TestStagedOptimizerRejectsLegacySemanticPlanOnlyRule(t *testing.T) {
	called := false
	plan := validStagedOptimizerContractPlan()

	_, err := optimizer.NewCanonical(legacyOnlyStagedRule{called: &called}).OptimizeSemanticPlan(t.Context(), plan)
	if err == nil || !strings.Contains(err.Error(), "does not implement the semantic graph contract") {
		t.Fatalf("legacy staged rule error = %v", err)
	}
	if called {
		t.Fatal("legacy semanticplan.SemanticPlan Apply was invoked by staged optimizer")
	}
	if got := plan.DenseCalendar.QueryTimeDimension; got != "order_date" {
		t.Fatalf("caller-owned graph mutated to %q", got)
	}
	if got := plan.DenseCalendar.QueryTimeDimension; got != "order_date" {
		t.Fatalf("caller-owned compatibility state mutated to %q", got)
	}
}

func TestStagedOptimizerDispatchesOnlyThroughThePlanDAGContract(t *testing.T) {
	plan := validStagedOptimizerContractPlan()

	optimized, err := optimizer.NewCanonical(graphOnlyDispatchRule{}).OptimizeSemanticPlan(t.Context(), plan)
	if err != nil {
		t.Fatalf("optimize staged graph: %v", err)
	}
	if got := optimized.DenseCalendar.QueryTimeDimension; got != "optimized_order_date" {
		t.Fatalf("optimized graph calendar dimension = %q", got)
	}
	if optimized.DenseCalendar != optimized.DenseCalendar {
		t.Fatal("compatibility calendar is not synchronized from canonical graph")
	}
	if got := optimized.DenseCalendar.QueryTimeDimension; got != "optimized_order_date" {
		t.Fatalf("compatibility calendar dimension = %q", got)
	}
	if got := plan.DenseCalendar.QueryTimeDimension; got != "order_date" {
		t.Fatalf("optimizer mutated caller-owned canonical graph to %q", got)
	}
	if got := plan.DenseCalendar.QueryTimeDimension; got != "order_date" {
		t.Fatalf("optimizer mutated caller-owned compatibility state to %q", got)
	}
}

func validStagedOptimizerContractPlan() *semanticplan.SemanticPlan {
	calendar := &semanticplan.DenseCalendarPlan{
		Dataset:            semanticplan.DatasetRef{Name: "time_spine", Source: "analytics.time_spine"},
		QueryTimeDimension: "order_date",
	}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue"}, DenseCalendar: semanticplan.CloneDenseCalendarPlan(calendar)}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "revenue",
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		Root:                      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		SourceRoots:               []string{"orders"},
		RequiredDatasets:          []string{"orders"},
	}}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Root:          semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Requested:     graph.Requested,
		DenseCalendar: calendar,
	}, semanticPlanNodeFixturesForTest(&graph))
	return plan
}
