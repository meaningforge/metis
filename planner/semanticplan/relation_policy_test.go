package semanticplan

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestRelationPolicyOwnsValuesAndScopeChangesScanIdentity(t *testing.T) {
	values := []any{"private"}
	input := []RelationPredicate{{Field: "tenant", Column: "tenant_id", Datatype: ossie.DataTypeString, Operator: query.FilterEQ, Values: values}}
	first, err := NewRelationPolicy("scope-a", "orders", input)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = "mutated"
	input[0].Field = "mutated"
	if first.Predicates()[0].Values[0] != "private" || first.Predicates()[0].Field != "tenant" {
		t.Fatal("constructor retained mutable storage")
	}
	copy := first.Predicates()
	copy[0].Values[0] = "changed"
	if first.Predicates()[0].Values[0] != "private" {
		t.Fatal("accessor exposed storage")
	}
	second, err := NewRelationPolicy("scope-b", "orders", first.Predicates())
	if err != nil {
		t.Fatal(err)
	}
	a := SourceScanWork{Root: DatasetRef{Name: "orders", Source: "orders", Policy: first}}
	b := a
	b.Root.Policy = second
	x, err := a.Identity()
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if x == y {
		t.Fatal("different policy scopes can share a scan")
	}
	same, err := NewRelationPolicy("scope-a", "orders", first.Predicates())
	if err != nil {
		t.Fatal(err)
	}
	b.Root.Policy = same
	y, err = b.Identity()
	if err != nil || x != y {
		t.Fatal("policy pointer identity affects scan sharing")
	}
	changed := first.Predicates()
	changed[0].Values[0] = "other"
	b.Root.Policy, err = NewRelationPolicy("scope-a", "orders", changed)
	if err != nil {
		t.Fatal(err)
	}
	y, err = b.Identity()
	if err != nil || x == y {
		t.Fatal("policy values absent from scan identity")
	}
}

func TestRelationPolicyCoverageAndRemovalFailClosed(t *testing.T) {
	policy, err := NewRelationPolicy("scope", "orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := &SemanticPlan{Nodes: []SemanticPlanNode{SourceSelectionNode{Source: SemanticSourceState{Root: DatasetRef{Name: "orders"}}}}}
	if _, err := WithRelationPolicies(plan, "scope", nil); err == nil {
		t.Fatal("missing coverage accepted")
	}
	bound, err := WithRelationPolicies(plan, "scope", map[string]*RelationPolicy{"orders": policy})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRelationPolicies(bound); err != nil {
		t.Fatal(err)
	}
	if plan.PolicyScope != "" || plan.Nodes[0].(SourceSelectionNode).Source.Root.Policy != nil {
		t.Fatal("installation mutated caller plan")
	}
	node := bound.Nodes[0].(SourceSelectionNode)
	node.Source.Root.Policy = nil
	bound.Nodes[0] = node
	if err := validateRelationPolicies(bound); err == nil {
		t.Fatal("lost source policy accepted")
	}
}
