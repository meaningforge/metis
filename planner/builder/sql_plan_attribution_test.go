package builder

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

func TestLoweringStrategySelectsAdditiveAttribution(t *testing.T) {
	plan := additiveAttributionSemanticPlanForTest(t)
	if got := conversion.LoweringStrategyForPlan(plan); got != conversion.SemanticLoweringAdditiveAttribution {
		t.Fatalf("lowering strategy = %q, want %q", got, conversion.SemanticLoweringAdditiveAttribution)
	}
	if !semanticplan.RequiresComposedPlan(plan) {
		t.Fatal("specialized attribution lowering must retain composed-plan invariants")
	}
}

func TestAdditiveAttributionAlignedBlockPreservesUnionPopulation(t *testing.T) {
	physical, err := conversion.BuildSQLPlan(attributionBundlePlanForTest(t, additiveAttributionRequestForTest(), "country"), mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	block := sqlPlanBlock(physical, additiveAttributionAlignedBlockID)
	if block == nil {
		t.Fatalf("aligned block %q is missing", additiveAttributionAlignedBlockID)
	}
	if block.ID != additiveAttributionAlignedBlockID {
		t.Fatalf("aligned block id = %q", block.ID)
	}
	if len(block.Inputs) != 2 || len(block.Joins) != 1 || block.Joins[0].Kind != sqlplan.JoinFullOuter {
		t.Fatalf("aligned block does not preserve baseline/current population union: %#v", block)
	}
	if len(block.Projections) != 4 {
		t.Fatalf("aligned projections = %d, want 4", len(block.Projections))
	}
	if block.Projections[0].Alias != "country" || block.Projections[1].Alias != additiveAttributionBaselineValueAlias || block.Projections[2].Alias != additiveAttributionCurrentValueAlias || block.Projections[3].Alias != additiveAttributionDeltaAlias {
		t.Fatalf("aligned projection contract = %#v", block.Projections)
	}
	if _, ok := block.Projections[0].Expr.(sqlplan.FunctionCallExpr); !ok {
		t.Fatalf("decomposition dimension is not population-coalesced: %#v", block.Projections[0].Expr)
	}
	delta, ok := block.Projections[3].Expr.(sqlplan.BinaryExpr)
	if !ok || delta.Operator != "-" {
		t.Fatalf("delta expression = %#v", block.Projections[3].Expr)
	}
	if _, ok := delta.Left.(sqlplan.FunctionCallExpr); !ok {
		t.Fatalf("current value is not zero-filled before subtraction: %#v", delta.Left)
	}
	if _, ok := delta.Right.(sqlplan.FunctionCallExpr); !ok {
		t.Fatalf("baseline value is not zero-filled before subtraction: %#v", delta.Right)
	}
}

func TestAdditiveAttributionTotalAndResultBlocksReconcile(t *testing.T) {
	physical, err := conversion.BuildSQLPlan(attributionBundlePlanForTest(t, additiveAttributionRequestForTest(), "country"), mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	total := sqlPlanBlock(physical, additiveAttributionTotalBlockID)
	if total == nil {
		t.Fatalf("total block %q is missing", additiveAttributionTotalBlockID)
	}
	if len(total.Projections) != 1 || total.Projections[0].Alias != additiveAttributionTotalDeltaAlias {
		t.Fatalf("total block projection = %#v", total.Projections)
	}
	sum, ok := total.Projections[0].Expr.(sqlplan.FunctionCallExpr)
	if !ok || strings.ToUpper(sum.Name) != "SUM" || len(sum.Args) != 1 {
		t.Fatalf("total delta is not SUM(delta): %#v", total.Projections[0].Expr)
	}

	root := sqlPlanBlock(physical, physical.Root)
	if root == nil {
		t.Fatalf("root block %q is missing", physical.Root)
	}
	if len(root.Joins) != 1 || root.Joins[0].Kind != sqlplan.JoinCross || root.Joins[0].On != nil {
		t.Fatalf("result block must cross join the scalar total: %#v", root.Joins)
	}
	if len(root.Projections) != 6 || root.Projections[5].Alias != additiveAttributionContributionAlias {
		t.Fatalf("result projection contract = %#v", root.Projections)
	}
	cast, ok := root.Projections[5].Expr.(sqlplan.CastExpr)
	if !ok || cast.Type != sqlplan.CastDecimal38Scale18 {
		t.Fatalf("contribution decimal cast = %#v", root.Projections[5].Expr)
	}
	multiply, ok := cast.Expr.(sqlplan.BinaryExpr)
	if !ok || multiply.Operator != "*" {
		t.Fatalf("contribution expression = %#v", root.Projections[5].Expr)
	}
	divide, ok := multiply.Left.(sqlplan.NullOnZeroDivideExpr)
	if !ok {
		t.Fatalf("contribution ratio = %#v", multiply.Left)
	}
	if denominator, ok := divide.Denominator.(sqlplan.ColumnRef); !ok || denominator.Name != additiveAttributionTotalDeltaAlias {
		t.Fatalf("zero-total guard = %#v", divide.Denominator)
	}
}
