//go:build duckdb

package duckdb

import (
	"context"
	"encoding/json"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend/internal/catalog"
	"github.com/meaningforge/metis/execution/driver"
)

func (e *executor) ValidateCompiled(ctx context.Context, compiled *artifact.CompiledQuery, limits driver.CatalogLimits) (driver.ValidationEvidence, error) {
	return catalog.Explain(ctx, e.db, compiled, limits, "duckdb")
}

func (*DriverFactory) SupportsCompiledValidation(compiled *artifact.CompiledQuery) bool {
	owned, err := artifact.SnapshotCompiledQuery(compiled)
	if err != nil || owned == nil {
		return false
	}
	// Native bind converts exact JSON numeric text through the same path as
	// production execution. The engine validates contextual parameter types.
	for _, p := range owned.SqlRenderResult.Parameters {
		if n, ok := p.Value.(json.Number); ok {
			if _, err := json.Marshal(n); err != nil {
				return false
			}
		}
	}
	return true
}
