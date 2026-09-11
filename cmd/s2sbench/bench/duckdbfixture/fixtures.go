//go:build duckdb

package fixture

import (
	"context"
	"fmt"
	"strings"

	enginefixture "github.com/meaningforge/metis/cmd/s2sbench/bench/enginefixture"
	conformance "github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

func (b *Backend) Prepare(ctx context.Context, scenario scenarios.Scenario) error {
	return b.PrepareFixture(ctx, scenario.Fixture)
}

func (b *Backend) PrepareFixture(ctx context.Context, id conformance.ID) error {
	statements, err := enginefixture.Statements(id, duckDBFixtureDialect{})
	if err != nil {
		return err
	}
	if err := b.resetSchema(ctx); err != nil {
		return err
	}
	return b.execute(ctx, statements...)
}

// resetSchema removes every table from the prior scenario. Conformance never
// relies on cross-fixture state, and S2SBench must not expose stale physical
// tables from an earlier scenario to the raw-assets arm.
func (b *Backend) resetSchema(ctx context.Context) error {
	return b.execute(ctx,
		"DROP SCHEMA IF EXISTS analytics CASCADE",
		"CREATE SCHEMA analytics",
	)
}

type duckDBFixtureDialect struct{}

func (duckDBFixtureDialect) CreateTable(table enginefixture.Table) (string, error) {
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		physicalType, err := duckDBFixtureType(column.Type)
		if err != nil {
			return "", err
		}
		columns[i] = column.Name + " " + physicalType
	}
	return "CREATE TABLE analytics." + table.Name + " (" + strings.Join(columns, ", ") + ")", nil
}

func duckDBFixtureType(logicalType enginefixture.LogicalType) (string, error) {
	switch logicalType {
	case enginefixture.String:
		return "VARCHAR", nil
	case enginefixture.Integer:
		return "BIGINT", nil
	case enginefixture.Float:
		return "DOUBLE", nil
	case enginefixture.Decimal:
		return "DECIMAL(20,12)", nil
	case enginefixture.Boolean:
		return "BOOLEAN", nil
	case enginefixture.Date:
		return "DATE", nil
	case enginefixture.DateTime:
		return "TIMESTAMP", nil
	default:
		return "", fmt.Errorf("unsupported DuckDB fixture type %q", logicalType)
	}
}
