package clickhouse

import (
	"context"
	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend/internal/catalog"
	"github.com/meaningforge/metis/execution/driver"
)

func (e *executor) ValidateCompiled(ctx context.Context, compiled *artifact.CompiledQuery, limits driver.CatalogLimits) (driver.ValidationEvidence, error) {
	if compiled == nil {
		return driver.ValidationEvidence{Outcome: "unsupported"}, nil
	}
	text, parameters, err := bindServerParameters(compiled.SqlRenderResult)
	if err != nil {
		return driver.ValidationEvidence{Outcome: "unsupported"}, nil
	}
	if len(parameters) > 0 {
		ctx = clickhousedriver.Context(ctx, clickhousedriver.WithParameters(parameters))
	}
	owned := *compiled
	owned.SqlRenderResult = compiled.SqlRenderResult
	owned.SqlRenderResult.SQL = text
	owned.SqlRenderResult.Parameters = nil
	return catalog.Explain(ctx, e.db, &owned, limits, "clickhouse")
}

// SupportsCompiledValidation is pure transport-shape checking before opening
// pools or resolving credentials. It uses the same binding as Execute.
func (*DriverFactory) SupportsCompiledValidation(compiled *artifact.CompiledQuery) bool {
	if compiled == nil {
		return false
	}
	_, _, err := bindServerParameters(compiled.SqlRenderResult)
	return err == nil
}
