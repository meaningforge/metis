package semanticplan_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

// The grain check is the one thing this phase can prove rather than merely
// relocate. For a staged plan the contract's grain comes from graph
// construction and plan.Groups comes from resolved dimensions -- two
// independent answers to one question -- so their agreement is a fact about the
// planner rather than a property of the types.
func TestOutputContractGrainMustMatchPlanGroups(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Groups: []semanticplan.GroupBy{{Name: "region"}},
		Output: semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{{Name: "region"}}},
	}
	if err := semanticplan.RequireOutputContractGrainMatchesPlan(plan); err != nil {
		t.Fatalf("matching grain was reported as a mismatch: %v", err)
	}

	plan.Output.Grain = []semanticplan.GroupBy{{Name: "country"}}
	err := semanticplan.RequireOutputContractGrainMatchesPlan(plan)
	if err == nil {
		t.Fatal("a plan grouping by one thing and reporting another was accepted")
	}
	if !strings.Contains(err.Error(), "region") || !strings.Contains(err.Error(), "country") {
		t.Errorf("error = %q, want it to name both grains", err)
	}

	plan.Output.Grain = nil
	if err := semanticplan.RequireOutputContractGrainMatchesPlan(plan); err == nil {
		t.Error("an empty contract grain was accepted against a grouped plan")
	}
}

// Four of the five contract fields are relocated rather than reconciled, so
// what matters for them is that the relocation is complete and owns its
// storage.
func TestOutputContractOwnsWhatItCollects(t *testing.T) {
	limit := 10
	plan := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "revenue", Kind: semanticplan.ProjectionMetric, Datasets: []string{"orders"}}},
		Groups:      []semanticplan.GroupBy{{Name: "region"}},
		Sorts:       []semanticplan.Sort{{Name: "revenue", Direction: query.SortDesc}},
		Limit:       &limit,
	}
	post := []semanticplan.PostEvaluationPredicate{{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 0}}}

	contract := semanticplan.BuildOutputContract(plan, plan.Groups, post)

	if len(contract.Projections) != 1 || len(contract.Grain) != 1 || len(contract.Predicates) != 1 || len(contract.OrderBy) != 1 {
		t.Fatalf("contract = %#v, want every output field collected", contract)
	}
	if contract.Limit == nil || *contract.Limit != 10 {
		t.Fatalf("limit = %v, want 10", contract.Limit)
	}

	// Owned, not shared. The contract is the owner while the flat fields remain
	// aliases; an owner that shares storage with its alias is just the alias.
	contract.Projections[0].Name = "mutated"
	contract.Projections[0].Datasets[0] = "mutated"
	contract.Grain[0].Name = "mutated"
	contract.OrderBy[0].Name = "mutated"
	*contract.Limit = 99

	if plan.Projections[0].Name != "revenue" || plan.Projections[0].Datasets[0] != "orders" {
		t.Error("the contract shares projection storage with the plan")
	}
	if plan.Groups[0].Name != "region" {
		t.Error("the contract shares grain storage with the plan")
	}
	if plan.Sorts[0].Name != "revenue" {
		t.Error("the contract shares ordering storage with the plan")
	}
	if *plan.Limit != 10 {
		t.Error("the contract shares its limit with the plan")
	}
}

func TestCloneSemanticOutputContractDoesNotAlias(t *testing.T) {
	limit := 5
	original := semanticplan.SemanticOutputContract{
		Projections: []semanticplan.Projection{{Name: "revenue", Datasets: []string{"orders"}}},
		Grain:       []semanticplan.GroupBy{{Name: "region"}},
		Predicates:  []semanticplan.PostEvaluationPredicate{{Name: "revenue"}},
		OrderBy:     []semanticplan.Sort{{Name: "revenue"}},
		Limit:       &limit,
	}

	cloned := semanticplan.CloneOutputContract(original)
	cloned.Projections[0].Datasets[0] = "mutated"
	cloned.Grain[0].Name = "mutated"
	cloned.Predicates[0].Name = "mutated"
	cloned.OrderBy[0].Name = "mutated"
	*cloned.Limit = 99

	if original.Projections[0].Datasets[0] != "orders" || original.Grain[0].Name != "region" ||
		original.Predicates[0].Name != "revenue" || original.OrderBy[0].Name != "revenue" || limit != 5 {
		t.Error("cloning the output contract left storage shared with the original")
	}
}
