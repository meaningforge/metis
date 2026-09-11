package conversion

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

func TestDecimalOutputContractPreservesFullIntegerRangeAndAvoidsDuplicateCast(t *testing.T) {
	semantic := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{{
		Name: "revenue", Kind: semanticplan.ProjectionMetric,
		Metric: &ossie.Metric{Name: "revenue", Datatype: ossie.DataTypeDecimal},
	}}}
	physical := &sqlplan.Plan{Root: "root", Blocks: []sqlplan.QueryBlock{{
		ID: "root", Projections: []sqlplan.Projection{{
			Alias: "revenue", Expr: sqlplan.OpaqueExpr{SQL: "SUM(revenue)", Dialect: sqlplan.ANSISQLExpressionDialect},
		}},
	}}}
	if err := applyOutputDatatypeContractsToSQLPlan(physical, semantic); err != nil {
		t.Fatal(err)
	}
	cast, ok := physical.Blocks[0].Projections[0].Expr.(sqlplan.CastExpr)
	if !ok || cast.Type != sqlplan.CastDecimal38Scale18 {
		t.Fatalf("root Decimal cast = %#v", physical.Blocks[0].Projections[0].Expr)
	}
	if _, narrowed := cast.Expr.(sqlplan.CastExpr); narrowed {
		t.Fatal("ordinary Decimal root was narrowed through an intermediate cast")
	}

	if err := applyOutputDatatypeContractsToSQLPlan(physical, semantic); err != nil {
		t.Fatal(err)
	}
	cast = physical.Blocks[0].Projections[0].Expr.(sqlplan.CastExpr)
	if _, duplicated := cast.Expr.(sqlplan.CastExpr); duplicated {
		t.Fatal("existing Decimal(38,18) output boundary was wrapped again")
	}
}

func TestRatioAttributionExactOperandsHonorOutputSchema(t *testing.T) {
	block := ratioAttributionResultBlock(semanticplan.RatioAttributionNode{DimensionRef: "segment"}, ossie.DataTypeDecimal, ossie.DataTypeDecimal)
	for _, index := range []int{3, 4, 5, 6} {
		cast, ok := block.Projections[index].Expr.(sqlplan.CastExpr)
		if !ok || cast.Type != sqlplan.CastDecimal38Scale18 {
			t.Fatalf("ratio attribution projection %q = %#v, want Decimal(38,18) cast", block.Projections[index].Alias, block.Projections[index].Expr)
		}
	}

	floatBlock := ratioAttributionResultBlock(semanticplan.RatioAttributionNode{DimensionRef: "segment"}, ossie.DataTypeFloat, ossie.DataTypeFloat)
	if _, cast := floatBlock.Projections[3].Expr.(sqlplan.CastExpr); cast {
		t.Fatal("Float ratio attribution operand received an exact Decimal output cast")
	}
}
