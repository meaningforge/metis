package builder

import (
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// The seam is a checked contract, so it has to fail for the reason it exists
// rather than merely return nil on today's corpus. Every conformance scenario
// exercises the unchanged case already; these cover construction bugs directly.
func TestSeamRejectsEvaluationSemanticsSettledDuringSourcePlanning(t *testing.T) {
	construction := ConstructionInput{Nodes: []semanticplan.SemanticPlanNode{
		semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
		semanticplan.SemiAdditiveNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue_last"}, Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "last"}},
	}}
	settled := CaptureEvaluationSeam(construction.Nodes)

	if err := RequireEvaluationSeam(settled, construction.Nodes); err != nil {
		t.Fatalf("unchanged evaluation identity was reported as moved: %v", err)
	}

	t.Run("kind", func(t *testing.T) {
		moved := cloneSeamInput(construction)
		moved.Nodes[1] = semanticplan.SemiAdditiveNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue_last"}, Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "first"}}

		err := RequireEvaluationSeam(settled, moved.Nodes)
		if err == nil {
			t.Fatal("a node kind settled by construction was allowed to change during source planning")
		}
		var typed *serrors.Error
		if !errors.As(err, &typed) {
			t.Fatalf("error = %T, want *serrors.Error", err)
		}
		nodes, _ := typed.Details["nodes"].(string)
		if !strings.Contains(nodes, "revenue_last") {
			t.Errorf("the error does not name the node that moved: %q", nodes)
		}
		if !strings.Contains(nodes, string(semanticplan.SemanticPlanNodeSemiAdditiveLast)) || !strings.Contains(nodes, string(semanticplan.SemanticPlanNodeSemiAdditiveFirst)) {
			t.Errorf("the error does not report what the identity moved from and to: %q", nodes)
		}
	})

	t.Run("variant", func(t *testing.T) {
		moved := cloneSeamInput(construction)
		moved.Nodes[0] = semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}}
		if err := RequireEvaluationSeam(settled, moved.Nodes); err == nil {
			t.Fatal("a typed node variant settled by construction was allowed to change during source planning")
		}
	})

	t.Run("added node", func(t *testing.T) {
		added := cloneSeamInput(construction)
		added.Nodes = append(added.Nodes, semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue_last__base"}})
		if err := RequireEvaluationSeam(settled, added.Nodes); err != nil {
			t.Errorf("a node added by source planning was rejected: %v", err)
		}
	})
}

func TestSemiAdditiveEvaluationKindIsDecidedFromThePolicy(t *testing.T) {
	for policy, want := range map[string]semanticplan.SemanticPlanNodeKind{
		"last":  semanticplan.SemanticPlanNodeSemiAdditiveLast,
		"first": semanticplan.SemanticPlanNodeSemiAdditiveFirst,
	} {
		kind, err := semiAdditiveEvaluationKind("revenue_snapshot", policy)
		if err != nil {
			t.Fatalf("%s: %v", policy, err)
		}
		if kind != want {
			t.Errorf("%s: kind = %s, want %s", policy, kind, want)
		}
	}

	if _, err := semiAdditiveEvaluationKind("revenue_snapshot", "median"); err == nil {
		t.Error("an unsupported semi-additive policy was accepted")
	}
}

func TestConstructedSourceRequirementFailsClosedForUnrecordedMetrics(t *testing.T) {
	input := ConstructionInput{SourceRequirements: map[string]SourceRequirement{
		"revenue": {Root: "orders", Datasets: []string{"orders"}},
	}}

	requirement, err := constructedMetricSourceRequirement(&input, "revenue")
	if err != nil {
		t.Fatalf("a recorded source requirement was not returned: %v", err)
	}
	if requirement.Root != "orders" || len(requirement.Datasets) != 1 || requirement.Datasets[0] != "orders" {
		t.Errorf("source requirement = %#v, want the recorded source facts", requirement)
	}

	if _, err := constructedMetricSourceRequirement(&input, "unrecorded"); err == nil {
		t.Error("source planning was allowed to proceed without a constructed source requirement")
	}
	if _, err := constructedMetricSourceRequirement(nil, "revenue"); err == nil {
		t.Error("a nil construction input was treated as a source requirement provider")
	}
}

func TestConstructedSourceRequirementDoesNotAliasRecordedFacts(t *testing.T) {
	input := ConstructionInput{SourceRequirements: map[string]SourceRequirement{
		"revenue": {Root: "orders", Datasets: []string{"orders"}},
	}}

	owned, err := constructedMetricSourceRequirement(&input, "revenue")
	if err != nil {
		t.Fatal(err)
	}
	owned.Datasets[0] = "mutated"

	if input.SourceRequirements["revenue"].Datasets[0] != "orders" {
		t.Error("the recorded source requirement shares backing storage with the copy handed to source planning")
	}
}

func cloneSeamInput(in ConstructionInput) ConstructionInput {
	out := in
	out.Nodes = append([]semanticplan.SemanticPlanNode(nil), in.Nodes...)
	return out
}
