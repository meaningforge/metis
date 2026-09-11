package clickhouse

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

func TestQuoteSourceRequiresDatabaseAndTable(t *testing.T) {
	got, err := (Renderer{}).QuoteSource("analytics.events")
	if err != nil {
		t.Fatal(err)
	}
	if got != "`analytics`.`events`" {
		t.Fatalf("quoted source = %q", got)
	}
	for _, source := range []string{"events", "sales.public.events", ""} {
		if _, err := (Renderer{}).QuoteSource(source); err == nil {
			t.Fatalf("QuoteSource(%q) succeeded", source)
		}
	}
}

func TestRenderExpressionOwnsClickHouseTimeAndOrderedValueSyntax(t *testing.T) {
	renderer := Renderer{}
	column := sqlplan.ColumnRef{Table: "events", Name: "occurred_at"}
	cases := []struct {
		name string
		expr sqlplan.Expr
		want string
	}{
		{name: "week", expr: sqlplan.TimeGrainExpr{Grain: query.TimeGrainWeek, Expr: column}, want: "toStartOfWeek(`events`.`occurred_at`, 1)"},
		{name: "quarter shift", expr: sqlplan.CalendarShiftExpr{Expr: column, Count: -1, Unit: query.TimeGrainQuarter}, want: "addQuarters(`events`.`occurred_at`, -1)"},
		{name: "deterministic earliest", expr: sqlplan.EarliestValueExpr{Value: sqlplan.ColumnRef{Name: "value"}, OrderBy: column, TieBreak: sqlplan.ColumnRef{Name: "sequence"}}, want: "argMin(`value`, tuple(`events`.`occurred_at`, `sequence`))"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderer.RenderExpression(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("expression = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderFullOuterJoinPreservesUnmatchedNulls(t *testing.T) {
	plan := &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{{
			ID:          "root",
			From:        sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.current"}, Alias: "current"},
			Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Table: "current", Name: "id"}, Alias: "id"}},
			Joins: []sqlplan.Join{{
				Kind:     sqlplan.JoinFullOuter,
				Relation: sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.previous"}, Alias: "previous"},
				On:       sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: "current", Name: "id"}, Operator: "=", Right: sqlplan.ColumnRef{Table: "previous", Name: "id"}},
			}},
		}},
	}
	query, err := (Renderer{}).Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query.SQL, "SETTINGS join_use_nulls = 1") {
		t.Fatalf("FULL OUTER JOIN did not preserve unmatched NULLs:\n%s", query.SQL)
	}
}

func TestRenderCastPreservesNullableExactDecimalBoundary(t *testing.T) {
	got, err := (Renderer{}).RenderCast("events.value", sqlplan.CastDecimal20Scale12)
	if err != nil {
		t.Fatal(err)
	}
	if got != "accurateCastOrNull(events.value, 'DECIMAL(20,12)')" {
		t.Fatalf("cast = %q", got)
	}
}
