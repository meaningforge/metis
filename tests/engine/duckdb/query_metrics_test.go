//go:build duckdb

package duckdb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/execution/backend"
	duckdbbackend "github.com/meaningforge/metis/execution/backend/duckdb"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
	duckdbfixture "github.com/meaningforge/metis/tests/engine/duckdb/fixture"
)

func TestQueryMetricsExecutesAdvancedSemanticsThroughProductionRuntime(t *testing.T) {
	for _, name := range []string{
		"multiple_metrics_time_grain",
		"cumulative_metric_by_quarter",
		"cumulative_metric_with_source_filter",
		"metric_filter_cumulative_metric",
		"time_offset_previous_year",
		"offset_to_grain_year_start_by_month_region",
		"semi_additive_last_skip_null",
		"semi_additive_queried_week",
		"semi_additive_window_group_sum",
		"conversion_count_by_campaign",
		"custom_calendar_rolling_three_fiscal_weeks",
		"custom_calendar_fiscal_quarter_to_date",
	} {
		scenario, ok := scenarios.ByName(name)
		if !ok {
			t.Fatalf("scenario %q is not registered", name)
		}
		t.Run(name, func(t *testing.T) {
			result := executeQueryMetricsScenario(t, scenario)
			assertQueryMetricsResult(t, scenario, result.Rows)
		})
	}
}

func executeQueryMetricsScenario(t *testing.T, scenario scenarios.Scenario) *service.QueryMetricsResult {
	t.Helper()
	ctx := context.Background()
	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		t.Fatalf("fixture %q is not registered", scenario.Fixture)
	}
	root := t.TempDir()
	database := filepath.Join(root, "runtime.duckdb")
	physical, err := duckdbfixture.New(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = physical.Close(context.Background()) })
	if err := physical.Prepare(ctx, scenario); err != nil {
		t.Fatal(err)
	}
	alignRuntimeFixtureTypes(t, ctx, physical, scenario.Fixture)

	writeRuntimeFile(t, filepath.Join(root, "model.ossie.yaml"), definition.Document)
	writeRuntimeFile(t, filepath.Join(root, "project.yaml"), []byte("semantic_sources:\n  model:\n    path: ./model.ossie.yaml\n"))
	writeRuntimeFile(t, filepath.Join(root, "datasources.yaml"), []byte(fmt.Sprintf(`warehouse:
  type: duckdb
  config:
    path: %q
  policy:
    query_timeout: 30s
    max_rows: 1000
    max_bytes: 8388608
    max_concurrency: 1
`, database)))
	writeRuntimeFile(t, filepath.Join(root, "metis.yaml"), []byte(fmt.Sprintf(`version: 1
projects:
  %s:
    path: ./project.yaml
    data_source: warehouse
data_sources:
  path: ./datasources.yaml
`, definition.Project)))
	backends, err := backend.NewBackendRegistry(duckdbbackend.New())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := bootstrap.LoadRuntime(filepath.Join(root, "metis.yaml"), bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Execution.Close(context.Background()) })
	result, err := runtime.QueryMetrics.QueryMetrics(ctx, service.QueryMetricsRequest{Query: scenario.Query})
	if err != nil {
		compiled, compileErr := runtime.Compile.Compile(ctx, service.CompileRequest{Query: scenario.Query, Dialect: "DUCKDB"})
		if compileErr != nil {
			t.Fatalf("query_metrics: %v; diagnostic compile: %v", err, compileErr)
		}
		_, executionErr := runtime.Execution.Execute(ctx, "warehouse", compiled, runner.ExecutionOptions{})
		t.Fatalf("query_metrics: %v; runtime execution: %v; schema=%#v; SQL:\n%s", err, executionErr, compiled.OutputSchema, compiled.SqlRenderResult.SQL)
	}
	if result.Count != int64(len(result.Rows)) || len(result.Schema.Columns) != len(scenario.ExpectedResult.Columns) {
		t.Fatalf("query_metrics result contract = %#v", result)
	}
	return result
}

func alignRuntimeFixtureTypes(t *testing.T, ctx context.Context, physical *duckdbfixture.Backend, fixture fixtures.ID) {
	t.Helper()
	// The generic real-engine corpus intentionally compares Float and Decimal as
	// one numeric family. query_metrics has the stricter production contract: an
	// Ossie Decimal must not arrive through a binary float, so align the selected
	// physical fixtures with their authored semantic datatype before execution.
	var statement string
	switch fixture {
	case fixtures.Commerce:
		statement = "ALTER TABLE analytics.orders ALTER COLUMN amount TYPE DECIMAL(20,12)"
	case fixtures.OffsetToGrain:
		statement = "ALTER TABLE analytics.offset_to_grain_orders ALTER COLUMN revenue TYPE DECIMAL(20,12)"
	case fixtures.SemiAdditiveNullSkip:
		statement = "ALTER TABLE analytics.inventory_snapshot_edges ALTER COLUMN quantity TYPE DECIMAL(20,12)"
	case fixtures.SemiAdditiveQueriedWindow, fixtures.SemiAdditiveWindowGrouping:
		statement = "ALTER TABLE analytics.inventory_snapshot_edges ALTER COLUMN quantity TYPE DECIMAL(20,12)"
	case fixtures.CustomCalendarRolling:
		statement = "ALTER TABLE analytics.rolling_orders ALTER COLUMN revenue TYPE DECIMAL(20,12)"
	case fixtures.CustomCalendarGrainToDate:
		statement = "ALTER TABLE analytics.gtd_orders ALTER COLUMN revenue TYPE DECIMAL(20,12)"
	case fixtures.Conversion:
		return
	default:
		t.Fatalf("runtime fixture %q has no physical type alignment", fixture)
	}
	if err := physical.Execute(ctx, statement); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertQueryMetricsResult(t *testing.T, scenario scenarios.Scenario, rows [][]any) {
	t.Helper()
	want := canonicalExpectedRows(t, scenario.ExpectedResult.Rows)
	got := canonicalRuntimeRows(t, scenario.ExpectedResult.Columns, rows)
	if scenario.ExpectedResult.Comparison == scenarios.ResultUnordered {
		sort.Strings(want)
		sort.Strings(got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query_metrics rows differ\nwant: %#v\ngot:  %#v", want, got)
	}
}

func canonicalExpectedRows(t *testing.T, rows []scenarios.ResultRow) []string {
	t.Helper()
	out := make([]string, len(rows))
	for rowIndex, row := range rows {
		values := make([]string, len(row))
		for columnIndex, value := range row {
			if value.Null {
				values[columnIndex] = scenarios.NullValue
			} else {
				values[columnIndex] = value.Canonical
			}
		}
		out[rowIndex] = fmt.Sprintf("%q", values)
	}
	return out
}

func canonicalRuntimeRows(t *testing.T, columns []scenarios.ResultColumn, rows [][]any) []string {
	t.Helper()
	out := make([]string, len(rows))
	for rowIndex, row := range rows {
		if len(row) != len(columns) {
			t.Fatalf("row %d has %d values, want %d", rowIndex, len(row), len(columns))
		}
		values := make([]string, len(row))
		for columnIndex, raw := range row {
			if raw == nil {
				values[columnIndex] = scenarios.NullValue
				continue
			}
			value, err := scenarios.ParseResultValue(columns[columnIndex].ValueKind, fmt.Sprint(raw))
			if err != nil {
				t.Fatalf("row %d column %q: %v", rowIndex, columns[columnIndex].Name, err)
			}
			values[columnIndex] = value.Canonical
		}
		out[rowIndex] = fmt.Sprintf("%q", values)
	}
	return out
}
