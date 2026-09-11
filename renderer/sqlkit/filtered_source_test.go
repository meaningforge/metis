package sqlkit_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

func TestFilteredRelationsPrecedeOuterJoinAndKeepValuesParameterized(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	const private = "tenant' OR 1=1 --"
	relation := func(name string) sqlplan.RelationRef {
		return sqlplan.RelationRef{Alias: name, FilteredSource: &sqlplan.FilteredTableSource{Name: "analytics." + name, Predicates: []sqlplan.Predicate{{Left: sqlplan.ColumnRef{Name: "tenant"}, Operator: query.FilterEQ, Values: []any{private}}}}}
	}
	plan := &sqlplan.Plan{Root: "root", Blocks: []sqlplan.QueryBlock{{ID: "root", From: relation("orders"),
		Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Table: "orders", Name: "id"}, Alias: "id"}},
		Joins:       []sqlplan.Join{{Kind: sqlplan.JoinFullOuter, Relation: relation("customers"), On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: "orders", Name: "id"}, Operator: "=", Right: sqlplan.ColumnRef{Table: "customers", Name: "id"}}}},
	}}}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []sql.SQLDialect{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(string(dialect), func(t *testing.T) {
			selected, err := registry.Resolve(dialect)
			if err != nil {
				t.Fatal(err)
			}
			result, err := selected.Render(plan)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.SQL, private) || strings.Count(result.SQL, "(SELECT * FROM") != 2 || !strings.Contains(result.SQL, "FULL OUTER JOIN") {
				t.Fatalf("unsafe relation lowering: %s", result.SQL)
			}
			if len(result.Parameters) != 2 || result.Parameters[0].Value != private || result.Parameters[1].Value != private {
				t.Fatal("policy parameters lost")
			}
			again, err := selected.Render(plan)
			if err != nil || !reflect.DeepEqual(result, again) {
				t.Fatal("rendering is not deterministic")
			}
			after, err := sqlplan.Fingerprint(plan)
			if err != nil || before != after {
				t.Fatal("renderer mutated policy inputs")
			}
		})
	}
	clone := sqlplan.Clone(plan)
	clone.Blocks[0].From.FilteredSource.Predicates[0].Values[0] = "changed"
	if plan.Blocks[0].From.FilteredSource.Predicates[0].Values[0] != private {
		t.Fatal("clone aliases policy values")
	}
	after, err := sqlplan.Fingerprint(clone)
	if err != nil || before == after {
		t.Fatal("policy values absent from physical identity")
	}
}
