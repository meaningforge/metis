package resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const customCalendarModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index"}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - name: day
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.day}]}
            dimension: {}
          - name: fiscal_week_start
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_week_start}]}
            dimension: {}
          - name: fiscal_week_index
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.fiscal_week_index}]}
            dimension: {}
          - name: revenue
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: calendar.revenue}]}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(calendar.revenue)"}]}
`

func TestResolveNamedCustomCalendarGrain(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarModel)
	grain := query.TimeGrain("fiscal_week")
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Model:      "fiscal",
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Dimensions) != 1 || resolved.Dimensions[0].CustomCalendar == nil {
		t.Fatalf("custom calendar not resolved: %#v", resolved.Dimensions)
	}
	calendar := resolved.Dimensions[0].CustomCalendar
	if calendar.Grain.Name != "fiscal_week" {
		t.Fatalf("grain = %#v", calendar.Grain)
	}
	if calendar.Dataset == nil || calendar.Dataset.Name != "calendar" {
		t.Fatalf("dataset = %#v", calendar.Dataset)
	}
	if calendar.BaseTimeField == nil || calendar.BaseTimeField.Name != "day" {
		t.Fatalf("base field = %#v", calendar.BaseTimeField)
	}
	if calendar.BucketField == nil || calendar.BucketField.Name != "fiscal_week_start" {
		t.Fatalf("bucket field = %#v", calendar.BucketField)
	}
	if calendar.OrdinalField == nil || calendar.OrdinalField.Name != "fiscal_week_index" {
		t.Fatalf("ordinal field = %#v", calendar.OrdinalField)
	}
}

func TestResolveCustomCalendarTargetExpressions(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarModel)
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
	if custom.BucketSelectedExpression != "calendar.fiscal_week_start" {
		t.Fatalf("bucket expression = %q", custom.BucketSelectedExpression)
	}
	if custom.OrdinalSelectedExpression != "calendar.fiscal_week_index" {
		t.Fatalf("ordinal expression = %q", custom.OrdinalSelectedExpression)
	}
}

func TestResolveBuiltInGrainDoesNotUseCustomCalendar(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarModel)
	grain := query.TimeGrainMonth
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "fiscal", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Dimensions[0].CustomCalendar != nil {
		t.Fatalf("built-in grain unexpectedly resolved through custom calendar: %#v", resolved.Dimensions[0].CustomCalendar)
	}
}

func TestRejectUnknownCustomCalendarGrain(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarModel)
	grain := query.TimeGrain("retail_week")
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "fiscal", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}}})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("error = %#v", err)
	}
}

func TestRejectCustomCalendarGrainOnNonBaseDimension(t *testing.T) {
	r := newResolverFromYAML(t, customCalendarModel)
	grain := query.TimeGrain("fiscal_week")
	_, err := r.Resolve(context.Background(), query.SemanticQuery{Model: "fiscal", Metrics: []query.MetricRef{{Name: "total_revenue"}}, Dimensions: []query.DimensionRef{{Name: "fiscal_week_start", Grain: &grain}}})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("error = %#v", err)
	}
}
