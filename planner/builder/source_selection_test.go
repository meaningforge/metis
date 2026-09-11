package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func TestSourceSelectionNodeDescribesTheQuery(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:   semanticplan.DatasetRef{Name: "customers", Source: "public.customers"},
		Joins:  []semanticplan.Join{{FromDataset: "customers", ToDataset: "regions"}},
		Groups: []semanticplan.GroupBy{{Name: "region"}},
		Predicates: []semanticplan.Predicate{
			{Filter: query.Filter{Field: "country", Operator: query.FilterEQ, Value: "JP"}},
		},
	}
	resolved := &resolver.ResolvedSemanticQuery{
		Intent:     query.QueryIntentDistinctValues,
		Dimensions: []resolver.ResolvedDimension{{Name: "region"}},
	}

	node, ok := BuildSourceSelection(plan, resolved)
	if !ok {
		t.Fatal("no node was built for a dimension query")
	}
	if node.Kind() != semanticplan.SemanticPlanNodeSourceSelection {
		t.Errorf("kind = %q, want %q", node.Kind(), semanticplan.SemanticPlanNodeSourceSelection)
	}
	if node.Mode != semanticplan.SourceSelectionDistinctValues {
		t.Errorf("node = %#v, want distinct_values source selection", node)
	}
	if node.Source.Root != plan.Root {
		t.Errorf("root = %#v, want the query root %#v", node.Source.Root, plan.Root)
	}
	if got := node.Source.RequiredDatasets; len(got) != 2 || got[0] != "customers" || got[1] != "regions" {
		t.Errorf("required datasets = %v, want [customers regions]", got)
	}
	if len(node.Base.Predicates) != 1 {
		t.Fatalf("node owns %d predicates, want the query's one", len(node.Base.Predicates))
	}
	predicate := node.Base.Predicates[0]
	if predicate.Scope != semanticplan.SemanticPredicateSourceRead {
		t.Errorf("scope = %q, want %q", predicate.Scope, semanticplan.SemanticPredicateSourceRead)
	}
	if predicate.OwnerNodeID != node.Base.ID {
		t.Errorf("owner = %q, want %q", predicate.OwnerNodeID, node.Base.ID)
	}
	if predicate.Proof != semanticplan.SemanticPredicateProofDatasetReachability {
		t.Errorf("proof = %q, want dataset reachability", predicate.Proof)
	}
	if err := semanticplan.ValidateNode(node); err != nil {
		t.Errorf("the constructed node does not validate: %v", err)
	}
}

func TestSourceSelectionModeDefaultsToGroupedValues(t *testing.T) {
	if mode := SourceSelectionMode(""); mode != semanticplan.SourceSelectionGroupedValues {
		t.Errorf("mode = %q, want %q", mode, semanticplan.SourceSelectionGroupedValues)
	}
	if mode := SourceSelectionMode(query.QueryIntentDistinctValues); mode != semanticplan.SourceSelectionDistinctValues {
		t.Errorf("mode = %q, want %q", mode, semanticplan.SourceSelectionDistinctValues)
	}
}

func TestNoSourceSelectionNodeWithoutDimensions(t *testing.T) {
	if _, ok := BuildSourceSelection(&semanticplan.SemanticPlan{}, &resolver.ResolvedSemanticQuery{}); ok {
		t.Error("a node was built for a query that selects nothing")
	}
	if _, ok := BuildSourceSelection(nil, &resolver.ResolvedSemanticQuery{Dimensions: []resolver.ResolvedDimension{{Name: "region"}}}); ok {
		t.Error("a node was built without a plan")
	}
}

func TestSourceSelectionNodeDoesNotAliasThePlan(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:   semanticplan.DatasetRef{Name: "customers"},
		Joins:  []semanticplan.Join{{FromDataset: "customers", ToDataset: "regions"}},
		Groups: []semanticplan.GroupBy{{Name: "region"}},
	}
	resolved := &resolver.ResolvedSemanticQuery{Dimensions: []resolver.ResolvedDimension{{Name: "region"}}}

	node, ok := BuildSourceSelection(plan, resolved)
	if !ok {
		t.Fatal("no node was built")
	}
	node.Source.Joins[0].ToDataset = "mutated"
	node.Base.OutputGrain[0].Name = "mutated"

	if plan.Joins[0].ToDataset != "regions" {
		t.Error("the node shares its join storage with the plan")
	}
	if plan.Groups[0].Name != "region" {
		t.Error("the node shares its grain storage with the plan")
	}
}
