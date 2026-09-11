package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

func TestSQLPlanSemiAdditiveComposableInputIsRetainedAndNormalized(t *testing.T) {
	base := semanticNodeFixture{
		ID:          "inventory_balance",
		Kind:        semanticplan.SemanticPlanNodeSemiAdditiveLast,
		OutputGrain: []semanticplan.GroupBy{{Name: "warehouse"}},
		Node: semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{
			BaseMetric:           "inventory_quantity",
			NonAdditiveDimension: "snapshot_date",
			Aggregation:          "last",
		}},
	}
	outer := semanticNodeFixture{
		ID:          "inventory_balance_reselected",
		Kind:        semanticplan.SemanticPlanNodeSemiAdditiveLast,
		Inputs:      []semanticNodeInputFixture{{NodeID: base.ID, Grain: base.OutputGrain}},
		OutputGrain: []semanticplan.GroupBy{{Name: "warehouse"}},
		Node: semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{
			BaseMetric:           base.ID,
			NonAdditiveDimension: "snapshot_date",
			Aggregation:          "last",
		}},
	}
	baseCTE := metricCTEName(0, base.ID)
	outerCTE := metricCTEName(1, outer.ID)
	physical := &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{
			{
				ID:   "base",
				From: sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "inventory"}, Alias: "inventory"},
				Projections: []sqlplan.Projection{
					{Expr: sqlplan.ColumnRef{Table: "inventory", Name: "warehouse"}, Alias: "warehouse"},
					{Expr: sqlplan.LatestValueExpr{Value: sqlplan.ColumnRef{Table: "inventory", Name: "quantity"}, OrderBy: sqlplan.ColumnRef{Table: "inventory", Name: "snapshot_date"}}, Alias: base.ID},
				},
				GroupBy: []sqlplan.Expr{sqlplan.ColumnRef{Table: "inventory", Name: "warehouse"}},
			},
			{
				ID:     "outer",
				Inputs: []sqlplan.QueryInput{{Alias: baseCTE, Block: "base", Mode: sqlplan.QueryInputCTE}},
				From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseCTE}, Alias: baseCTE},
				Projections: []sqlplan.Projection{
					{Expr: sqlplan.ColumnRef{Table: baseCTE, Name: "warehouse"}, Alias: "warehouse"},
					{Expr: sqlplan.LatestValueExpr{Value: sqlplan.ColumnRef{Table: baseCTE, Name: base.ID}, OrderBy: sqlplan.ColumnRef{Table: baseCTE, Name: "snapshot_date"}}, Alias: outer.ID},
				},
				GroupBy: []sqlplan.Expr{sqlplan.ColumnRef{Table: baseCTE, Name: "warehouse"}},
			},
			{
				ID: "root",
				Inputs: []sqlplan.QueryInput{
					{Alias: baseCTE, Block: "base", Mode: sqlplan.QueryInputCTE},
					{Alias: outerCTE, Block: "outer", Mode: sqlplan.QueryInputCTE},
				},
				From:        sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: outerCTE}, Alias: outerCTE},
				Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Table: outerCTE, Name: outer.ID}, Alias: outer.ID}},
			},
		},
	}
	semantic := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{base, outer})

	if err := conversion.ApplySemiAdditiveComposabilityToSQLPlan(physical, semantic); err != nil {
		t.Fatal(err)
	}
	if err := conversion.NormalizeSemiAdditiveComposableInputsToSQLPlan(physical, semantic); err != nil {
		t.Fatal(err)
	}

	root := sqlPlanBlock(physical, physical.Root)
	consumer := sqlPlanInputBlock(physical, root, outerCTE)
	inputAlias := semiAdditiveStateInput(outer.ID)
	if consumer == nil || consumer.From.Input == nil || consumer.From.Input.Alias != inputAlias {
		t.Fatalf("composed selector source = %#v, want %q", consumer, inputAlias)
	}
	if len(consumer.Joins) != 0 {
		t.Fatalf("composed selector still has state joins: %#v", consumer.Joins)
	}
	input := sqlPlanInputBlock(physical, root, inputAlias)
	if input == nil || len(input.Joins) != 1 || input.Joins[0].Relation.Input == nil || input.Joins[0].Relation.Input.Alias != semiAdditiveStateOrderInput(base.ID) {
		t.Fatalf("normalized selector input = %#v", input)
	}
	selector, ok := consumer.Projections[1].Expr.(sqlplan.LatestValueExpr)
	if !ok {
		t.Fatalf("composed selector = %T", consumer.Projections[1].Expr)
	}
	order, ok := selector.OrderBy.(sqlplan.ColumnRef)
	if !ok || order.Table != inputAlias || order.Name != semiAdditiveStateOrderColumn(base.ID) {
		t.Fatalf("composed selector order input = %#v", selector.OrderBy)
	}
}
