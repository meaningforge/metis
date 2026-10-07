package clickhouse

import (
	"context"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend/internal/catalog"
	"github.com/meaningforge/metis/execution/driver"
)

func (e *executor) ValidateCompiled(ctx context.Context, compiled *artifact.CompiledQuery, limits driver.CatalogLimits) (driver.ValidationEvidence, error) {
	return catalog.Explain(ctx, e.db, compiled, limits, "clickhouse")
}
