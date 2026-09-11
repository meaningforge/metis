package sqlplan_test

import (
	"testing"

	"github.com/meaningforge/metis/sqlplan"
)

func conditionalPlan() *sqlplan.Plan {
	present := sqlplan.ColumnRef{Table: "events", Name: "present"}
	value := sqlplan.ColumnRef{Table: "events", Name: "value"}
	expr := sqlplan.CaseExpr{
		Branches: []sqlplan.CaseWhen{{
			When: sqlplan.NullTestExpr{Expr: present, Negated: true},
			Then: sqlplan.ParenthesizedExpr{Expr: sqlplan.BinaryExpr{
				Left:     value,
				Operator: "+",
				Right:    sqlplan.OpaqueExpr{SQL: "1", Dialect: sqlplan.ANSISQLExpressionDialect},
			}},
		}},
		Else: sqlplan.OpaqueExpr{SQL: "NULL", Dialect: sqlplan.ANSISQLExpressionDialect},
	}
	return &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{{
			ID:          "root",
			From:        sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "events"}, Alias: "events"},
			Projections: []sqlplan.Projection{{Expr: expr, Alias: "defined_value"}},
		}},
	}
}

func TestConditionalExpressionsValidateCloneAndFingerprint(t *testing.T) {
	plan := conditionalPlan()
	if err := sqlplan.Validate(plan); err != nil {
		t.Fatal(err)
	}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	cloned := sqlplan.Clone(plan)
	caseExpr := cloned.Blocks[0].Projections[0].Expr.(sqlplan.CaseExpr)
	caseExpr.Branches[0].Then = sqlplan.OpaqueExpr{SQL: "2", Dialect: sqlplan.ANSISQLExpressionDialect}
	cloned.Blocks[0].Projections[0].Expr = caseExpr
	after, err := sqlplan.Fingerprint(cloned)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("conditional expression change did not change SQLPlan fingerprint")
	}
	original := plan.Blocks[0].Projections[0].Expr.(sqlplan.CaseExpr)
	if _, ok := original.Branches[0].Then.(sqlplan.ParenthesizedExpr); !ok {
		t.Fatal("SQLPlan clone mutated caller-owned case branches")
	}
}

func TestConditionalExpressionsRejectIncompleteStructure(t *testing.T) {
	plan := conditionalPlan()
	expr := plan.Blocks[0].Projections[0].Expr.(sqlplan.CaseExpr)
	expr.Branches = nil
	plan.Blocks[0].Projections[0].Expr = expr
	if err := sqlplan.Validate(plan); err == nil {
		t.Fatal("branchless case expression unexpectedly validated")
	}
}

func TestCastExpressionValidatesClonesAndFingerprints(t *testing.T) {
	plan := conditionalPlan()
	plan.Blocks[0].Projections[0].Expr = sqlplan.CastExpr{
		Expr: sqlplan.ColumnRef{Table: "events", Name: "value"},
		Type: sqlplan.CastDecimal38Scale18,
	}
	if err := sqlplan.Validate(plan); err != nil {
		t.Fatal(err)
	}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	cloned := sqlplan.Clone(plan)
	cast := cloned.Blocks[0].Projections[0].Expr.(sqlplan.CastExpr)
	cast.Expr = sqlplan.OpaqueExpr{SQL: "2", Dialect: sqlplan.ANSISQLExpressionDialect}
	cloned.Blocks[0].Projections[0].Expr = cast
	after, err := sqlplan.Fingerprint(cloned)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("cast expression change did not change SQLPlan fingerprint")
	}
	original := plan.Blocks[0].Projections[0].Expr.(sqlplan.CastExpr)
	if _, ok := original.Expr.(sqlplan.ColumnRef); !ok {
		t.Fatal("SQLPlan clone mutated caller-owned cast expression")
	}

	cast.Type = sqlplan.CastType("FLOAT")
	plan.Blocks[0].Projections[0].Expr = cast
	if err := sqlplan.Validate(plan); err == nil {
		t.Fatal("unsupported cast type unexpectedly validated")
	}
}

func TestNullOnZeroDivideValidatesClonesAndFingerprints(t *testing.T) {
	plan := conditionalPlan()
	plan.Blocks[0].Projections[0].Expr = sqlplan.NullOnZeroDivideExpr{
		Numerator:   sqlplan.ColumnRef{Table: "events", Name: "value"},
		Denominator: sqlplan.ColumnRef{Table: "events", Name: "present"},
	}
	if err := sqlplan.Validate(plan); err != nil {
		t.Fatal(err)
	}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	cloned := sqlplan.Clone(plan)
	division := cloned.Blocks[0].Projections[0].Expr.(sqlplan.NullOnZeroDivideExpr)
	division.Denominator = sqlplan.OpaqueExpr{SQL: "2", Dialect: sqlplan.ANSISQLExpressionDialect}
	cloned.Blocks[0].Projections[0].Expr = division
	after, err := sqlplan.Fingerprint(cloned)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("null-on-zero division change did not change SQLPlan fingerprint")
	}
	original := plan.Blocks[0].Projections[0].Expr.(sqlplan.NullOnZeroDivideExpr)
	if _, ok := original.Denominator.(sqlplan.ColumnRef); !ok {
		t.Fatal("SQLPlan clone mutated caller-owned division expression")
	}

	division.Denominator = nil
	plan.Blocks[0].Projections[0].Expr = division
	if err := sqlplan.Validate(plan); err == nil {
		t.Fatal("division without denominator unexpectedly validated")
	}
}
