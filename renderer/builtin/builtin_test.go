package builtin_test

import (
	"go/build"
	"reflect"
	"sync"
	"testing"

	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/clickhouse"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/renderer/duckdb"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

func TestPackageOwnsOnlyOfficialRendererComposition(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pkg.GoFiles, []string{"builtin.go"}) {
		t.Fatalf("production files = %v, want only builtin composition", pkg.GoFiles)
	}
	if !reflect.DeepEqual(pkg.XTestGoFiles, []string{"builtin_test.go"}) {
		t.Fatalf("external test files = %v, keep concrete Renderer tests in their owning packages", pkg.XTestGoFiles)
	}
}

func TestRenderersReturnsOwnedOfficialSet(t *testing.T) {
	first := builtin.Renderers()
	second := builtin.Renderers()
	want := []sql.SQLDialect{duckdb.Dialect, doris.Dialect, clickhouse.Dialect}
	if len(first) != len(want) || len(second) != len(want) {
		t.Fatalf("renderer counts = %d, %d, want %d", len(first), len(second), len(want))
	}
	for i := range want {
		if first[i] == nil || second[i] == nil || first[i].SQLDialect() != want[i] || second[i].SQLDialect() != want[i] {
			t.Fatalf("renderer %d = %#v, %#v, want dialect %q", i, first[i], second[i], want[i])
		}
		if reflect.TypeOf(first[i]) != reflect.TypeOf(second[i]) {
			t.Fatalf("renderer %q types = %T, %T", want[i], first[i], second[i])
		}
	}
	first[0] = nil
	if second[0] == nil {
		t.Fatal("Renderers returned shared slice storage")
	}
}

func TestOfficialRenderersAreConcurrentDeterministicAndPlanImmutable(t *testing.T) {
	for _, target := range builtin.Renderers() {
		t.Run(string(target.SQLDialect()), func(t *testing.T) {
			assertRendererContract(t, target)
		})
	}
}

func assertRendererContract(t *testing.T, target renderer.Renderer) {
	t.Helper()
	plan := &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{{
			ID:   "root",
			From: sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.orders"}, Alias: "orders"},
			Projections: []sqlplan.Projection{{
				Expr:  sqlplan.OpaqueExpr{SQL: "SUM(orders.amount)", Dialect: target.ExpressionDialect()},
				Alias: "revenue",
			}},
		}},
	}
	before, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	want, err := target.Render(plan)
	if err != nil {
		t.Fatal(err)
	}

	var group sync.WaitGroup
	errors := make(chan error, 8)
	queries := make(chan sql.SQLQuery, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			query, renderErr := target.Render(plan)
			if renderErr != nil {
				errors <- renderErr
				return
			}
			queries <- query
		}()
	}
	group.Wait()
	close(errors)
	close(queries)
	for err := range errors {
		t.Fatal(err)
	}
	for query := range queries {
		if !reflect.DeepEqual(query, want) {
			t.Fatalf("concurrent Render() = %#v, want %#v", query, want)
		}
	}
	after, err := sqlplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("Renderer mutated caller-owned SQLPlan")
	}
}
