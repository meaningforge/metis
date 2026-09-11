package compiler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

// cumulativeOverBase rewrites the commerce fixture so cumulative_revenue
// accumulates a base metric with the given aggregation expression, and compiles
// it at a monthly grain.
func cumulativeOverBase(t *testing.T, baseExpression string) (sql.SqlRenderResult, error) {
	t.Helper()
	const originalBase = `      - name: cumulative_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "revenue"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"unbounded"}}'`

	rewritten := `      - name: rollup_base
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "` + baseExpression + `"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"order_date"}'
      - name: cumulative_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "rollup_base"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"cumulative","base_metric":"rollup_base","time_dimension":"order_date","window":{"type":"unbounded"}}'`

	source := string(fixtures.CommerceModelYAML)
	if !strings.Contains(source, originalBase) {
		t.Fatal("commerce fixture no longer declares cumulative_revenue in the expected shape")
	}
	doc, err := ossie.NewLoader().Load([]byte(strings.Replace(source, originalBase, rewritten, 1)))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	grain := query.TimeGrainMonth
	semanticQuery := query.SemanticQuery{
		Project:    projectName,
		Model:      modelName,
		Metrics:    []query.MetricRef{{Name: "cumulative_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
	}
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, renderer)
	if err != nil {
		t.Fatal("resolve:", err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		return sql.SqlRenderResult{}, err
	}
	sqlQuery, err := compilePlan(context.Background(), plan, renderer)
	if err != nil {
		return sql.SqlRenderResult{}, err
	}
	return sqlQuery, nil
}

// A cumulative metric merges its base metric's partial results. Which operator
// does that correctly is a property of the base's aggregation. Using the constant SUM as the merge operator would make so a running maximum compiled to
// SUM(MAX(...)) OVER (...) -- a running sum of per-period maxima, with nothing
// in the output to suggest it was not the requested number.
func TestCumulativeMergesWithTheBaseAggregationsOperator(t *testing.T) {
	for _, tt := range []struct {
		name           string
		baseExpression string
		wantMerge      string
	}{
		{name: "sum merges with sum", baseExpression: "SUM(orders.amount)", wantMerge: "SUM("},
		{name: "count merges with sum, not count", baseExpression: "COUNT(orders.order_id)", wantMerge: "SUM("},
		{name: "max merges with max", baseExpression: "MAX(orders.amount)", wantMerge: "MAX("},
		{name: "min merges with min", baseExpression: "MIN(orders.amount)", wantMerge: "MIN("},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sqlQuery, err := cumulativeOverBase(t, tt.baseExpression)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			window := windowFunctionOver(t, sqlQuery.SQL, "rollup_base")
			if !strings.HasPrefix(window, tt.wantMerge) {
				t.Fatalf("cumulative window merges with %q, want %q\n%s", window, tt.wantMerge, sqlQuery.SQL)
			}
		})
	}
}

// COUNT deserves its own statement of the trap. Merging counts by counting them
// would return the number of partials rather than the number of rows.
func TestCumulativeCountDoesNotMergeByCounting(t *testing.T) {
	sqlQuery, err := cumulativeOverBase(t, "COUNT(orders.order_id)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(sqlQuery.SQL, "COUNT(`metric_001_rollup_base`.`rollup_base`)") {
		t.Fatalf("cumulative count merged by counting its partials\n%s", sqlQuery.SQL)
	}
}

// An aggregation whose partial state the evaluated column does not retain, or
// which cannot be computed from partials at all, must fail closed rather than
// produce a plausible wrong number.
func TestCumulativeFailsClosedOnUnmergeableBase(t *testing.T) {
	for _, tt := range []struct {
		name           string
		baseExpression string
		wantAlgebra    string
	}{
		// Algebraic: AVG could be merged from (sum, count), but the base CTE
		// emits the finished ratio, so those partials are already gone.
		{name: "average", baseExpression: "AVG(orders.amount)", wantAlgebra: "ALGEBRAIC"},
		// Holistic: no partial state makes a distinct count mergeable.
		{name: "distinct count", baseExpression: "COUNT(DISTINCT orders.customer_id)", wantAlgebra: "HOLISTIC"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cumulativeOverBase(t, tt.baseExpression)

			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidMetricRollup {
				t.Fatalf("error = %#v, want %s", err, serrors.ErrInvalidMetricRollup)
			}
			if got := apiErr.Details["rollup_algebra"]; got != tt.wantAlgebra {
				t.Fatalf("rollup_algebra = %#v, want %q", got, tt.wantAlgebra)
			}
			if got := apiErr.Details["base_metric"]; got != "rollup_base" {
				t.Fatalf("base_metric = %#v", got)
			}
			// The model author is the only one who can act on this, and the
			// error must say so rather than sending a caller to edit a query.
			if got := serrors.CallerActionOf(apiErr.Code); got != serrors.CallerActionChangeModel {
				t.Fatalf("caller action = %q, want CHANGE_MODEL", got)
			}
		})
	}
}

// windowFunctionOver extracts the function name applied to the given column
// inside an OVER clause, so a test can assert the merge operator without
// pinning the whole statement.
func windowFunctionOver(t *testing.T, sql, column string) string {
	t.Helper()
	for _, line := range strings.Split(sql, "\n") {
		if !strings.Contains(line, "OVER (") || !strings.Contains(line, column) {
			continue
		}
		return strings.TrimSpace(line)
	}
	t.Fatalf("no window function over %q found in\n%s", column, sql)
	return ""
}
