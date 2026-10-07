package clickhouse

import (
	"context"
	"github.com/meaningforge/metis/execution/backend/internal/catalog"
	"github.com/meaningforge/metis/execution/driver"
)

func (e *executor) DescribeRelation(ctx context.Context, ref driver.CatalogReference, limits driver.CatalogLimits) (driver.CatalogRelation, error) {
	return catalog.Describe(ctx, e.db, "clickhouse", ref, limits, catalog.ParseClickHouse)
}
