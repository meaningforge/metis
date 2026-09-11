package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestValidateStructuralNonRegressionRejectsProtectedGrowth(t *testing.T) {
	before := semanticPlanForTest(semanticplan.SemanticPlan{
		Joins: []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
	}, []semanticNodeFixture{{
		ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "orders", Metrics: []string{"revenue"},
	}})

	after := semanticplan.ClonePlan(before)
	afterNodes := semanticPlanNodeFixturesForTest(after)
	afterNodes = append(afterNodes, semanticTestNodeFixture(semanticNodeFixture{
		ID: "duplicate_revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "duplicate", Metrics: []string{"revenue"},
	}))
	replaceSemanticPlanNodeFixturesForTest(after, afterNodes)

	err := semanticplan.ValidateStructuralNonRegression(before, after)
	if err == nil || !strings.Contains(err.Error(), "source groups increased") {
		t.Fatalf("expected source-group regression, got %v", err)
	}
}

func TestDefaultOptimizerDoesNotRegressProtectedStructure(t *testing.T) {
	duplicate := semanticplan.Join{FromDataset: "orders", ToDataset: "customers"}
	before := &semanticplan.SemanticPlan{
		Root:  semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Joins: []semanticplan.Join{duplicate, duplicate},
		Projections: []semanticplan.Projection{{
			Name:     "customer_id",
			Kind:     semanticplan.ProjectionDimension,
			Dataset:  "customers",
			Datasets: []string{"customers"},
		}},
	}

	after, err := optimizer.Default().Optimize(context.Background(), before)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if err := semanticplan.ValidateStructuralNonRegression(before, after); err != nil {
		t.Fatalf("default optimizer regressed protected structure: %v", err)
	}
	if got, want := semanticplan.StructuralQuality(after).Joins, 1; got != want {
		t.Fatalf("optimized joins = %d, want %d", got, want)
	}
}

func TestStructuralQualityIgnoresOptimizerTrace(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Joins: []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
	}
	before := semanticplan.StructuralQuality(plan)
	plan.OptimizationTrace = append(plan.OptimizationTrace, semanticplan.OptimizationStep{Rule: "test", Changed: true})
	if after := semanticplan.StructuralQuality(plan); after != before {
		t.Fatalf("trace changed structural quality: before=%+v after=%+v", before, after)
	}
}
