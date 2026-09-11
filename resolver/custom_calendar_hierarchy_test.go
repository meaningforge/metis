package resolver_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarHierarchyModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","parent_grain":"fiscal_quarter"},{"name":"fiscal_quarter","bucket_dimension":"fiscal_quarter_start","ordinal_dimension":"fiscal_quarter_index","parent_grain":"fiscal_year"},{"name":"fiscal_year","bucket_dimension":"fiscal_year_start","ordinal_dimension":"fiscal_year_index"}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - {name: day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.day}]}, dimension: {}}
          - {name: fiscal_week_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_week_start}]}, dimension: {}}
          - {name: fiscal_week_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_week_index}]}, dimension: {}}
          - {name: fiscal_quarter_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_quarter_start}]}, dimension: {}}
          - {name: fiscal_quarter_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_quarter_index}]}, dimension: {}}
          - {name: fiscal_year_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_year_start}]}, dimension: {}}
          - {name: fiscal_year_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_year_index}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.revenue}]}}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(calendar.revenue)"}]}
`

func TestResolveCustomCalendarHierarchyExpressions(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarHierarchyModel)
	grain := query.TimeGrain("fiscal_week")
	resolved, err := r.ResolveForRenderer(context.Background(), query.SemanticQuery{
		Model:      "fiscal",
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}},
	}, mustRenderer(t, "CLICKHOUSE"))
	if err != nil {
		t.Fatal(err)
	}
	custom := resolved.Dimensions[0].CustomCalendar
	if custom == nil {
		t.Fatal("custom calendar not resolved")
	}
	quarter := custom.Levels["fiscal_quarter"]
	if quarter == nil {
		t.Fatal("fiscal quarter hierarchy level not resolved")
	}
	if quarter.BucketField == nil || quarter.BucketField.Name != "fiscal_quarter_start" {
		t.Fatalf("quarter bucket = %#v", quarter.BucketField)
	}
	if quarter.BucketSelectedExpression != "calendar.fiscal_quarter_start" {
		t.Fatalf("quarter bucket expression = %q", quarter.BucketSelectedExpression)
	}
	year := custom.Levels["fiscal_year"]
	if year == nil || year.OrdinalSelectedExpression != "calendar.fiscal_year_index" {
		t.Fatalf("year hierarchy level = %#v", year)
	}
}
