package compiler

import (
	"context"
	"fmt"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

// MetricAttributionCompiledQuery associates one compiled query with its
// decomposition dimension.
type MetricAttributionCompiledQuery struct {
	Dimension string `json:"dimension"`
	*artifact.CompiledQuery
}

// MetricAttributionCompilation is the compiled form of one canonical metric
// attribution bundle.
type MetricAttributionCompilation struct {
	Request attribution.ResolvedMetricAttributionRequest `json:"request"`
	Queries []MetricAttributionCompiledQuery             `json:"queries"`
}

// CompileMetricAttributionBundleResolved compiles every plan in a canonical
// attribution bundle with one already-selected Renderer.
func (c *Compiler) CompileMetricAttributionBundleResolved(ctx context.Context, bundle *attribution.MetricAttributionBundle, selected renderer.Renderer) (*MetricAttributionCompilation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric attribution bundle is required"}
	}
	if selected == nil {
		return nil, &serrors.Error{Code: serrors.ErrTargetRequired, Message: "selected Renderer is required"}
	}
	if c == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "compiler is not configured"}
	}
	canonical, err := attribution.BuildMetricAttributionBundle(bundle.Request, bundle.Queries)
	if err != nil {
		return nil, err
	}
	compiled := make([]MetricAttributionCompiledQuery, 0, len(canonical.Queries))
	for _, query := range canonical.Queries {
		physicalQuery, compileErr := c.CompileWithRenderer(ctx, query.Plan, selected)
		if compileErr != nil {
			return nil, fmt.Errorf("compile metric attribution dimension %q: %w", query.Dimension, compileErr)
		}
		compiled = append(compiled, MetricAttributionCompiledQuery{Dimension: query.Dimension, CompiledQuery: physicalQuery})
	}
	request := canonical.Request
	request.Dimensions = append([]string(nil), canonical.Request.Dimensions...)
	request.Filters = append([]semanticplan.Predicate(nil), canonical.Request.Filters...)
	return &MetricAttributionCompilation{Request: request, Queries: compiled}, nil
}

// CompileMetricAttributionBundle selects one Renderer and uses that same
// instance to compile every plan in the attribution bundle.
func (c *Compiler) CompileMetricAttributionBundle(ctx context.Context, bundle *attribution.MetricAttributionBundle, dialect sql.SQLDialect) (*MetricAttributionCompilation, error) {
	if bundle == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric attribution bundle with semantic plans is required"}
	}
	if c == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "compiler is not configured"}
	}
	canonical, err := attribution.BuildMetricAttributionBundle(bundle.Request, bundle.Queries)
	if err != nil {
		return nil, err
	}
	selected, err := c.ResolveRenderer(dialect)
	if err != nil {
		return nil, err
	}
	return c.CompileMetricAttributionBundleResolved(ctx, canonical, selected)
}
