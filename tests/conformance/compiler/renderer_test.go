package compiler_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := registry.Resolve(sql.SQLDialect(dialect))
	if err == nil {
		return renderer
	}
	return unsupportedTestRenderer{dialect: sql.SQLDialect(dialect)}
}

type unsupportedTestRenderer struct{ dialect sql.SQLDialect }

func (r unsupportedTestRenderer) SQLDialect() sql.SQLDialect { return r.dialect }
func (r unsupportedTestRenderer) ExpressionDialect() string  { return string(r.dialect) }
func (unsupportedTestRenderer) Capabilities() renderer.Capabilities {
	return renderer.Capabilities{}
}
func (r unsupportedTestRenderer) Render(*sqlplan.Plan) (sql.SQLRenderResult, error) {
	return sql.SQLRenderResult{}, fmt.Errorf("SQL dialect %q is not supported", r.dialect)
}

func compilePlan(ctx context.Context, plan *semanticplan.SemanticPlan, renderer renderer.Renderer) (sql.SQLRenderResult, error) {
	if _, unsupported := renderer.(unsupportedTestRenderer); unsupported {
		return sql.SQLRenderResult{}, &serrors.Error{Code: serrors.ErrUnsupportedDialect, Message: "selected SQL dialect is not registered"}
	}
	compiled, err := compiler.CompileWithRenderer(ctx, plan, renderer)
	if err != nil {
		return sql.SQLRenderResult{}, err
	}
	return compiled.PhysicalQuery, nil
}
