package sqlplan_test

import (
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

// Every expression field projected by SQLPlan can affect SQL or parameter
// identity. This gate is deliberately explicit: adding a field to an
// expression requires adding a movement case rather than relying on reflection
// or whole-object serialization to notice it by accident.
func TestFingerprintMovesForEveryExpressionField(t *testing.T) {
	column := func(name string) sqlplan.Expr { return sqlplan.ColumnRef{Table: "orders", Name: name} }
	opaque := func(sql, dialect string) sqlplan.Expr { return sqlplan.OpaqueExpr{SQL: sql, Dialect: dialect} }
	function := func(name string, args ...sqlplan.Expr) sqlplan.FunctionCallExpr {
		return sqlplan.FunctionCallExpr{Name: name, Args: args}
	}

	tests := []struct {
		name   string
		before sqlplan.Expr
		after  sqlplan.Expr
	}{
		{name: "opaque SQL", before: opaque("amount", "DUCKDB"), after: opaque("amount + 1", "DUCKDB")},
		{name: "opaque selection dialect", before: opaque("amount", "DUCKDB"), after: opaque("amount", sqlplan.ANSISQLExpressionDialect)},
		{name: "column table", before: sqlplan.ColumnRef{Name: "amount"}, after: column("amount")},
		{name: "column name", before: column("amount"), after: column("quantity")},
		{name: "binary left", before: sqlplan.BinaryExpr{Left: column("amount"), Operator: "+", Right: column("tax")}, after: sqlplan.BinaryExpr{Left: column("quantity"), Operator: "+", Right: column("tax")}},
		{name: "binary operator", before: sqlplan.BinaryExpr{Left: column("amount"), Operator: "+", Right: column("tax")}, after: sqlplan.BinaryExpr{Left: column("amount"), Operator: "-", Right: column("tax")}},
		{name: "binary right", before: sqlplan.BinaryExpr{Left: column("amount"), Operator: "+", Right: column("tax")}, after: sqlplan.BinaryExpr{Left: column("amount"), Operator: "+", Right: column("discount")}},
		{name: "logical operator", before: sqlplan.LogicalExpr{Operator: "AND", Terms: []sqlplan.Expr{column("amount")}}, after: sqlplan.LogicalExpr{Operator: "OR", Terms: []sqlplan.Expr{column("amount")}}},
		{name: "logical terms", before: sqlplan.LogicalExpr{Operator: "AND", Terms: []sqlplan.Expr{column("amount")}}, after: sqlplan.LogicalExpr{Operator: "AND", Terms: []sqlplan.Expr{column("amount"), column("tax")}}},
		{name: "function name", before: function("SUM", column("amount")), after: function("MAX", column("amount"))},
		{name: "function args", before: function("SUM", column("amount")), after: function("SUM", column("quantity"))},
		{name: "time grain", before: sqlplan.TimeGrainExpr{Grain: query.TimeGrainMonth, Expr: column("created_at")}, after: sqlplan.TimeGrainExpr{Grain: query.TimeGrainYear, Expr: column("created_at")}},
		{name: "time grain expression", before: sqlplan.TimeGrainExpr{Grain: query.TimeGrainMonth, Expr: column("created_at")}, after: sqlplan.TimeGrainExpr{Grain: query.TimeGrainMonth, Expr: column("updated_at")}},
		{name: "calendar shift expression", before: sqlplan.CalendarShiftExpr{Expr: column("created_at"), Count: 1, Unit: query.TimeGrainMonth}, after: sqlplan.CalendarShiftExpr{Expr: column("updated_at"), Count: 1, Unit: query.TimeGrainMonth}},
		{name: "calendar shift count", before: sqlplan.CalendarShiftExpr{Expr: column("created_at"), Count: 1, Unit: query.TimeGrainMonth}, after: sqlplan.CalendarShiftExpr{Expr: column("created_at"), Count: 2, Unit: query.TimeGrainMonth}},
		{name: "calendar shift unit", before: sqlplan.CalendarShiftExpr{Expr: column("created_at"), Count: 1, Unit: query.TimeGrainMonth}, after: sqlplan.CalendarShiftExpr{Expr: column("created_at"), Count: 1, Unit: query.TimeGrainYear}},
		{name: "latest value", before: sqlplan.LatestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.LatestValueExpr{Value: column("quantity"), OrderBy: column("created_at")}},
		{name: "latest order", before: sqlplan.LatestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.LatestValueExpr{Value: column("amount"), OrderBy: column("updated_at")}},
		{name: "latest tie break", before: sqlplan.LatestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.LatestValueExpr{Value: column("amount"), OrderBy: column("created_at"), TieBreak: column("id")}},
		{name: "earliest value", before: sqlplan.EarliestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.EarliestValueExpr{Value: column("quantity"), OrderBy: column("created_at")}},
		{name: "earliest order", before: sqlplan.EarliestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.EarliestValueExpr{Value: column("amount"), OrderBy: column("updated_at")}},
		{name: "earliest tie break", before: sqlplan.EarliestValueExpr{Value: column("amount"), OrderBy: column("created_at")}, after: sqlplan.EarliestValueExpr{Value: column("amount"), OrderBy: column("created_at"), TieBreak: column("id")}},
		{name: "window function", before: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}, after: sqlplan.WindowExpr{Function: function("MAX", column("amount")), Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}},
		{name: "window partition", before: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}, after: sqlplan.WindowExpr{Function: function("SUM", column("amount")), PartitionBy: []sqlplan.Expr{column("region")}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}},
		{name: "window order", before: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}, after: sqlplan.WindowExpr{Function: function("SUM", column("amount")), OrderBy: []sqlplan.Expr{column("created_at")}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}},
		{name: "window frame", before: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}, after: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsPrecedingToCurrent}},
		{name: "window preceding rows", before: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsPrecedingToCurrent, PrecedingRows: 1}, after: sqlplan.WindowExpr{Function: function("SUM", column("amount")), Frame: sqlplan.WindowRowsPrecedingToCurrent, PrecedingRows: 2}},
		{name: "row number partition", before: sqlplan.RowNumberExpr{OrderBy: []sqlplan.Order{{Expr: column("created_at"), Direction: query.SortDesc}}}, after: sqlplan.RowNumberExpr{PartitionBy: []sqlplan.Expr{column("region")}, OrderBy: []sqlplan.Order{{Expr: column("created_at"), Direction: query.SortDesc}}}},
		{name: "row number order expression", before: sqlplan.RowNumberExpr{OrderBy: []sqlplan.Order{{Expr: column("created_at"), Direction: query.SortDesc}}}, after: sqlplan.RowNumberExpr{OrderBy: []sqlplan.Order{{Expr: column("updated_at"), Direction: query.SortDesc}}}},
		{name: "row number order direction", before: sqlplan.RowNumberExpr{OrderBy: []sqlplan.Order{{Expr: column("created_at"), Direction: query.SortDesc}}}, after: sqlplan.RowNumberExpr{OrderBy: []sqlplan.Order{{Expr: column("created_at"), Direction: query.SortAsc}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := expressionPlan(test.before)
			after := expressionPlan(test.after)
			beforeFingerprint, err := sqlplan.Fingerprint(before)
			if err != nil {
				t.Fatalf("fingerprint before: %v", err)
			}
			afterFingerprint, err := sqlplan.Fingerprint(after)
			if err != nil {
				t.Fatalf("fingerprint after: %v", err)
			}
			if beforeFingerprint == afterFingerprint {
				t.Fatal("field movement did not move SQLPlan fingerprint")
			}
		})
	}
}

func expressionPlan(expr sqlplan.Expr) *sqlplan.Plan {
	plan := validPlan()
	plan.Blocks = plan.Blocks[:1]
	plan.Root = plan.Blocks[0].ID
	plan.Blocks[0].Projections[0].Expr = expr
	return plan
}
