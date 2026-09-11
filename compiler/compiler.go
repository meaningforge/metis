package compiler

import (
	"context"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

// Compiler owns the single Renderer selection boundary for compile-only use.
// The caller supplies one SQLDialect; no project/runtime binding participates.
type Compiler struct {
	renderers RendererResolver
}

// RendererResolver is the single SQLDialect-to-Renderer lookup boundary used by
// one compilation. Downstream stages receive the selected Renderer directly.
type RendererResolver interface {
	Resolve(sql.SQLDialect) (renderer.Renderer, error)
}

func NewCompiler(renderers RendererResolver) *Compiler {
	return &Compiler{renderers: renderers}
}

func (c *Compiler) ResolveRenderer(dialect sql.SQLDialect) (renderer.Renderer, error) {
	if c == nil || c.renderers == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "compiler is not configured"}
	}
	if dialect == "" {
		return nil, &serrors.Error{Code: serrors.ErrTargetRequired, Message: "SQL dialect is required"}
	}
	renderer, err := c.renderers.Resolve(dialect)
	if err != nil {
		return nil, &serrors.Error{Code: serrors.ErrUnsupportedDialect, Message: fmt.Sprintf("SQL dialect %q is not supported", dialect), Details: map[string]any{"cause": err.Error()}}
	}
	return renderer, nil
}

func (c *Compiler) CompileWithRenderer(ctx context.Context, plan *semanticplan.SemanticPlan, selected renderer.Renderer) (*artifact.CompiledQuery, error) {
	return CompileWithRenderer(ctx, plan, selected)
}

// CompileWithRenderer lowers and renders one already-resolved semantic plan
// with the caller-supplied selected Renderer. It performs no registry lookup.
func CompileWithRenderer(ctx context.Context, plan *semanticplan.SemanticPlan, selected renderer.Renderer) (*artifact.CompiledQuery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan is required"}
	}
	if selected == nil {
		return nil, &serrors.Error{Code: serrors.ErrTargetRequired, Message: "selected Renderer is required"}
	}
	_ = selected.Capabilities()
	outputSchema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		return nil, err
	}
	physicalPlan, err := conversion.BuildSQLPlan(plan, selected)
	if err != nil {
		return nil, err
	}
	query, err := selected.Render(physicalPlan)
	if err != nil {
		return nil, serrors.Internal("Renderer could not render a validated SQLPlan", map[string]any{"dialect": selected.SQLDialect(), "cause": err.Error()})
	}
	if query.Dialect == "" || query.Dialect != selected.SQLDialect() || query.SQL == "" {
		return nil, serrors.Internal("Renderer produced an invalid physical SQL query", map[string]any{"dialect": selected.SQLDialect()})
	}
	compiled, err := artifact.NewCompiledQuery(query, outputSchema)
	if err != nil {
		return nil, serrors.Internal("Renderer produced an invalid compiler artifact", map[string]any{"dialect": selected.SQLDialect()})
	}
	return compiled, nil
}

func (c *Compiler) Compile(ctx context.Context, plan *semanticplan.SemanticPlan, dialect sql.SQLDialect) (*artifact.CompiledQuery, error) {
	if plan == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan is required"}
	}
	selected, err := c.ResolveRenderer(dialect)
	if err != nil {
		return nil, err
	}
	return c.CompileWithRenderer(ctx, plan, selected)
}
