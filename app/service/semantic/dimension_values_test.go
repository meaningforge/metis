package semantic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func TestDimensionValuesNormalizesClosedMetricFreeAndMetricScopedQueries(t *testing.T) {
	service := dimensionValuesServiceForTest(t, nil)
	limit := 2
	metricFree, err := service.normalizeDimensionValues(context.Background(), DimensionValuesQuery{
		ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Limit: &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery := metricFree.semanticQuery()
	if semanticQuery.Intent != query.QueryIntentDistinctValues || len(semanticQuery.Metrics) != 0 || semanticQuery.Model != "sales" || semanticQuery.Project != "analytics" {
		t.Fatalf("metric-free query = %#v", semanticQuery)
	}
	if len(semanticQuery.Dimensions) != 1 || semanticQuery.Dimensions[0].Name != "orders.region" || semanticQuery.Dimensions[0].Grain != nil {
		t.Fatalf("metric-free dimensions = %#v", semanticQuery.Dimensions)
	}
	if len(semanticQuery.Filters) != 1 || semanticQuery.Filters[0].Field != "orders.region" || semanticQuery.Filters[0].Operator != query.FilterIsNotNull || semanticQuery.Filters[0].Value != nil {
		t.Fatalf("metric-free filters = %#v", semanticQuery.Filters)
	}
	if len(semanticQuery.OrderBy) != 1 || semanticQuery.OrderBy[0].Field != "orders.region" || semanticQuery.OrderBy[0].Direction != query.SortAsc || semanticQuery.Limit == nil || *semanticQuery.Limit != 3 {
		t.Fatalf("metric-free ordering/limit = %#v / %#v", semanticQuery.OrderBy, semanticQuery.Limit)
	}

	metricScoped, err := service.normalizeDimensionValues(context.Background(), DimensionValuesQuery{
		ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"metric:sales.revenue", "metric:sales.orders_count"},
	})
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery = metricScoped.semanticQuery()
	if semanticQuery.Intent != "" || len(semanticQuery.Metrics) != 2 || semanticQuery.Metrics[0].Name != "revenue" || semanticQuery.Metrics[1].Name != "orders_count" || semanticQuery.Limit == nil || *semanticQuery.Limit != 101 {
		t.Fatalf("metric-scoped query = %#v", semanticQuery)
	}
}

func TestDimensionValuesRejectsInvalidPublicRequestsBeforeExecution(t *testing.T) {
	service := dimensionValuesServiceForTest(t, nil)
	grain := query.TimeGrainMonth
	zero, tooLarge := 0, 501
	tests := []struct {
		name  string
		input DimensionValuesQuery
		code  serrors.ErrorCode
	}{
		{name: "bare dimension", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "orders.region"}, code: serrors.ErrInvalidQuery},
		{name: "bare metric", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"revenue"}}, code: serrors.ErrInvalidQuery},
		{name: "duplicate metrics", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"metric:sales.revenue", "metric:sales.revenue"}}, code: serrors.ErrInvalidQuery},
		{name: "cross model", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"metric:inventory.stock"}}, code: serrors.ErrInvalidQuery},
		{name: "incompatible dimension", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"metric:sales.customer_count"}}, code: serrors.ErrMetricSourceUnreachable},
		{name: "grain on category", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Grain: &grain}, code: serrors.ErrInvalidQuery},
		{name: "opaque", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.payload"}, code: serrors.ErrInvalidQuery},
		{name: "zero limit", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Limit: &zero}, code: serrors.ErrInvalidQuery},
		{name: "large limit", input: DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Limit: &tooLarge}, code: serrors.ErrInvalidQuery},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.normalizeDimensionValues(context.Background(), test.input)
			if errorCode(err) != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}

	nine := make([]string, 9)
	for index := range nine {
		nine[index] = "metric:sales.revenue"
	}
	_, err := service.normalizeDimensionValues(context.Background(), DimensionValuesQuery{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: nine})
	if errorCode(err) != serrors.ErrInvalidQuery {
		t.Fatalf("excessive metric error = %v", err)
	}
}

func TestDimensionValuesProjectsTypedValuesAndExactTruncation(t *testing.T) {
	normalized := normalizedDimensionValues{dimension: "orders.country", publicDimension: "dimension:sales.orders.country", limit: 2, metrics: []normalizedDimensionValueMetric{{internal: "revenue"}}}
	schema := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: "orders.country", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		{Name: "revenue", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	}}
	result := runner.ResultSet{Schema: schema, Rows: [][]any{{"CN", "1.00"}, {"DE", "2.00"}, {"JP", "3.00"}}, Count: 3}
	values, truncated, dataType, err := projectDimensionValues(result, schema, normalized)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || dataType != ossie.DataTypeString || len(values) != 2 || values[0] != "CN" || values[1] != "DE" {
		t.Fatalf("projected result = values:%#v truncated:%v datatype:%s", values, truncated, dataType)
	}

	duplicate := result
	duplicate.Rows = [][]any{{"CN", "1.00"}, {"CN", "2.00"}}
	duplicate.Count = 2
	if _, _, _, err := projectDimensionValues(duplicate, schema, normalized); errorCode(err) != serrors.ErrInternalInvariant {
		t.Fatalf("duplicate error = %v", err)
	}
	numericSchema := artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "orders.amount", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeDecimal}}}
	numericDuplicate := runner.ResultSet{Schema: numericSchema, Rows: [][]any{{"1.0"}, {"1.00"}}, Count: 2}
	numericNormalized := normalizedDimensionValues{dimension: "orders.amount", publicDimension: "dimension:sales.orders.amount", limit: 2}
	if _, _, _, err := projectDimensionValues(numericDuplicate, numericSchema, numericNormalized); errorCode(err) != serrors.ErrInternalInvariant {
		t.Fatalf("numerically equivalent duplicate error = %v", err)
	}

	empty := runner.ResultSet{Schema: artifact.OutputSchema{Columns: schema.Columns[:1]}, Rows: nil, Count: 0}
	metricFree := normalized
	metricFree.metrics = nil
	values, truncated, _, err = projectDimensionValues(empty, empty.Schema, metricFree)
	if err != nil || values == nil || len(values) != 0 || truncated {
		t.Fatalf("empty result = values:%#v truncated:%v err:%v", values, truncated, err)
	}

	for _, item := range []struct {
		dataType ossie.DataType
		value    any
	}{
		{ossie.DataTypeString, "US"}, {ossie.DataTypeInteger, json.Number("7")}, {ossie.DataTypeDecimal, "7.25"},
		{ossie.DataTypeFloat, json.Number("7.25")}, {ossie.DataTypeBoolean, true}, {ossie.DataTypeDate, "2026-09-02"},
		{ossie.DataTypeTime, "12:30:00"}, {ossie.DataTypeDateTime, "2026-09-02T12:30:00"}, {ossie.DataTypeDateTimeTz, "2026-09-02T12:30:00Z"},
	} {
		if !validNormalizedDimensionValue(item.value, item.dataType) {
			t.Fatalf("normalized value rejected: datatype=%s value=%#v", item.dataType, item.value)
		}
	}
}

func TestDimensionValuesUsesResolvedBackendRendererAndOneAtomicExecution(t *testing.T) {
	semanticManifest := dimensionValuesManifestForTest(t)
	store := manifest.NewStore(semanticManifest)
	selected := &identityRenderer{delegate: mustRenderer(t, "DORIS")}
	lookup := &countingRendererResolver{renderer: selected}
	compileService := NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(lookup)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})

	maxRows, maxBytes := int64(10), int64(4096)
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"warehouse": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: intPointer(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: selected, DriverFactory: queryMetricsDriver{}})
	if err != nil {
		t.Fatal(err)
	}
	service := NewDimensionValuesService(compileService, NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{}), queryMetricsProjects{}, runner.New(sources, backends, nil, nil))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.GetDimensionValues(ctx, DimensionValuesQuery{Dimension: "dimension:sales.orders.region"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QueryID == "" || result.Dimension != "dimension:sales.orders.region" || result.DataType != ossie.DataTypeString || result.Count != 1 || result.Values[0] != "12.50" || result.Truncated {
		t.Fatalf("result = %#v", result)
	}
	if lookup.calls != 0 || selected.renderCalls != 1 || selected.expressionCalls == 0 || selected.capabilitiesCalls == 0 {
		t.Fatalf("Renderer authority = lookup:%d expression:%d capabilities:%d render:%d", lookup.calls, selected.expressionCalls, selected.capabilitiesCalls, selected.renderCalls)
	}
}

func TestDimensionValuesRequiresExecuteScope(t *testing.T) {
	service := NewDimensionValuesService(nil, nil, nil, nil)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticCompile}})
	_, err := service.GetDimensionValues(ctx, DimensionValuesQuery{Dimension: "dimension:sales.orders.region"})
	if errorCode(err) != serrors.ErrProjectAccessDenied {
		t.Fatalf("get_dimension_values without execute scope error = %v", err)
	}
}

func TestDimensionValuesCompilesCanonicalCustomCalendarGrain(t *testing.T) {
	semanticManifest := dimensionValuesCustomCalendarManifestForTest(t)
	store := manifest.NewStore(semanticManifest)
	selected := &identityRenderer{delegate: mustRenderer(t, "DORIS")}
	compileService := NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(&countingRendererResolver{renderer: selected})).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	service := NewDimensionValuesService(compileService, NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{}), queryMetricsProjects{}, nil).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	grain := query.TimeGrain("fiscal_week")
	normalized, err := service.normalizeDimensionValues(context.Background(), DimensionValuesQuery{
		ProjectID: "analytics", Dimension: "dimension:fiscal_dense.calendar.calendar_day", Grain: &grain,
		Metrics: []string{"metric:fiscal_dense.revenue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileService.compileWithRenderer(context.Background(), CompileRequest{Query: normalized.semanticQuery()}, selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDimensionValuesSchema(compiled.OutputSchema, normalized); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SqlStatement.SQL, "fiscal_week_start") || compiled.OutputSchema.Columns[0].Grain == nil || *compiled.OutputSchema.Columns[0].Grain != grain {
		t.Fatalf("custom-calendar compilation = SQL:%s schema:%#v", compiled.SqlStatement.SQL, compiled.OutputSchema)
	}
	_, err = service.normalizeDimensionValues(context.Background(), DimensionValuesQuery{
		ProjectID: "analytics", Dimension: "dimension:fiscal_dense.calendar.fiscal_week_start", Grain: &grain,
	})
	if errorCode(err) != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("physical bucket misuse error = %v", err)
	}
}

func TestDimensionValuesQueryShapesCompileAcrossBuiltInRenderers(t *testing.T) {
	semanticManifest := dimensionValuesManifestForTest(t)
	store := manifest.NewStore(semanticManifest)
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	compileService := NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(registry)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	service := NewDimensionValuesService(compileService, NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{}), queryMetricsProjects{}, nil).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	requests := []DimensionValuesQuery{
		{ProjectID: "analytics", Dimension: "dimension:sales.orders.region"},
		{ProjectID: "analytics", Dimension: "dimension:sales.orders.region", Metrics: []string{"metric:sales.revenue", "metric:sales.orders_count"}},
	}
	for _, dialect := range []sql.SQLDialect{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		selected, resolveErr := registry.Resolve(dialect)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		for _, request := range requests {
			normalized, normalizeErr := service.normalizeDimensionValues(context.Background(), request)
			if normalizeErr != nil {
				t.Fatal(normalizeErr)
			}
			compiled, compileErr := compileService.compileWithRenderer(context.Background(), CompileRequest{Query: normalized.semanticQuery()}, selected)
			if compileErr != nil {
				t.Fatalf("dialect=%s metrics=%d: %v", dialect, len(request.Metrics), compileErr)
			}
			if schemaErr := validateDimensionValuesSchema(compiled.OutputSchema, normalized); schemaErr != nil {
				t.Fatalf("dialect=%s metrics=%d: %v", dialect, len(request.Metrics), schemaErr)
			}
			for _, fragment := range []string{"IS NOT NULL", "ORDER BY"} {
				if !strings.Contains(compiled.SqlStatement.SQL, fragment) {
					t.Fatalf("dialect=%s metrics=%d SQL omits %q: %s", dialect, len(request.Metrics), fragment, compiled.SqlStatement.SQL)
				}
			}
			if !strings.Contains(compiled.SqlStatement.SQL, "LIMIT") && !strings.Contains(compiled.SqlStatement.SQL, "FETCH FIRST") {
				t.Fatalf("dialect=%s metrics=%d SQL omits bounded limit: %s", dialect, len(request.Metrics), compiled.SqlStatement.SQL)
			}
		}
	}
}

func dimensionValuesServiceForTest(t *testing.T, executionRuntime *runner.Runner) *DimensionValuesService {
	t.Helper()
	store := manifest.NewStore(dimensionValuesManifestForTest(t))
	return NewDimensionValuesService(nil, NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{}), queryMetricsProjects{}, executionRuntime).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
}

func dimensionValuesManifestForTest(t *testing.T) *manifest.SemanticManifest {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.region}]}, dimension: {}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
          - {name: payload, datatype: Opaque, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.payload}]}, dimension: {}}
      - name: customers
        source: analytics.customers
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customers.customer_id}]}, dimension: {}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]}
      - name: customer_count
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: COUNT(customers.customer_id)}]}
      - name: orders_count
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: COUNT(orders.amount)}]}
  - name: inventory
    datasets:
      - name: stock
        source: analytics.stock
        fields:
          - {name: quantity, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: stock.quantity}]}}
    metrics:
      - name: stock
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(stock.quantity)}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	return semanticManifest
}

func dimensionValuesCustomCalendarManifestForTest(t *testing.T) *manifest.SemanticManifest {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_dense
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"calendar_day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true}]}'
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: order_day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: order_day}]}, dimension: {is_time: true}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
      - name: calendar
        source: analytics.calendar
        primary_key: [calendar_day]
        fields:
          - {name: calendar_day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar_day}]}, dimension: {is_time: true}}
          - {name: fiscal_week_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}, dimension: {}}
          - {name: fiscal_week_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}, dimension: {}}
    relationships:
      - {name: orders_to_calendar, from: orders, to: calendar, from_columns: [order_day], to_columns: [calendar_day]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"calendar_day"}'}
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	return semanticManifest
}
