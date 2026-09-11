package ossie

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestMetricDefinitionFilterContract(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"definition_filter","filters":[{"field":"status","operator":"eq","value":"paid"},{"field":"deleted_at","operator":"is_null"}]}`,
	}}}
	spec, ok, err := MetricDefinitionFilter(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("definition-filter extension was not detected")
	}
	if EffectiveMetricDefinitionFilterStage(spec) != MetricDefinitionFilterStagePreAggregation {
		t.Fatalf("stage = %q", EffectiveMetricDefinitionFilterStage(spec))
	}
	if len(spec.Filters) != 2 || spec.Filters[0].Field != "status" || spec.Filters[1].Operator != query.FilterIsNull {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestMetricDefinitionFilterValidation(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "empty filters", data: `{"kind":"definition_filter","filters":[]}`},
		{name: "missing field", data: `{"kind":"definition_filter","filters":[{"operator":"eq","value":"paid"}]}`},
		{name: "unknown operator", data: `{"kind":"definition_filter","filters":[{"field":"status","operator":"matches","value":"paid"}]}`},
		{name: "missing value", data: `{"kind":"definition_filter","filters":[{"field":"status","operator":"eq"}]}`},
		{name: "null operator value", data: `{"kind":"definition_filter","filters":[{"field":"status","operator":"is_null","value":"paid"}]}`},
		{name: "unknown stage", data: `{"kind":"definition_filter","stage":"dependency_input","filters":[{"field":"status","operator":"eq","value":"paid"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metric := &Metric{CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: tc.data}}}
			if _, _, err := MetricDefinitionFilter(metric); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMetricDefinitionFilterModelValidation(t *testing.T) {
	valid := []byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: status
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.status}]}
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","filters":[{"field":"status","operator":"eq","value":"paid"}]}'
`)
	if _, err := NewLoader().Load(valid); err != nil {
		t.Fatalf("valid model: %v", err)
	}

	missing := []byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","filters":[{"field":"status","operator":"eq","value":"paid"}]}'
`)
	_, err := NewLoader().Load(missing)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidModel {
		t.Fatalf("error = %#v, want INVALID_MODEL", err)
	}
}

func TestMetricDefinitionFilterRejectsAmbiguousField(t *testing.T) {
	model := &SemanticModel{Name: "sales", Datasets: []Dataset{
		{Name: "orders", Source: "analytics.orders", Fields: []Field{{Name: "status"}}},
		{Name: "customers", Source: "analytics.customers", Fields: []Field{{Name: "status"}}},
	}}
	spec := MetricDefinitionFilterSpec{Kind: MetricExtensionDefinitionFilter, Filters: []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}}}
	if err := ValidateMetricDefinitionFilterModel(model, "paid_revenue", spec); err == nil {
		t.Fatal("expected ambiguous field error")
	}
}

func TestPostAggregationDefinitionFilterTargetsOwningMetric(t *testing.T) {
	model := &SemanticModel{Name: "sales"}
	valid := MetricDefinitionFilterSpec{
		Kind:    MetricExtensionDefinitionFilter,
		Stage:   MetricDefinitionFilterStagePostAggregation,
		Filters: []query.Filter{{Field: "conversion_rate", Operator: query.FilterGTE, Value: 0.5}},
	}
	if err := ValidateMetricDefinitionFilterModel(model, "conversion_rate", valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Filters = []query.Filter{{Field: "revenue", Operator: query.FilterGTE, Value: 100}}
	if err := ValidateMetricDefinitionFilterModel(model, "conversion_rate", invalid); err == nil {
		t.Fatal("expected foreign metric target to fail")
	}
}

func TestPostAggregationDefinitionFilterLoadsOnDerivedMetric(t *testing.T) {
	model := []byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
          - name: orders_count
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.orders_count}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: order_count
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.orders_count)"}]}
      - name: average_order_value
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "revenue / order_count"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","stage":"post_aggregation","filters":[{"field":"average_order_value","operator":"gte","value":50}]}'
`)
	if _, err := NewLoader().Load(model); err != nil {
		t.Fatalf("post-aggregation definition filter model: %v", err)
	}
}
