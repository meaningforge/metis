package ossie

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestCumulativeSpec(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"unbounded"}}`,
	}}}
	spec, ok, err := CumulativeSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("cumulative extension was not detected")
	}
	if spec.BaseMetric != "revenue" || spec.TimeDimension != "order_date" || spec.Window.Type != "unbounded" {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestRollingCumulativeSpec(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","count":3,"unit":"month"}}`,
	}}}
	spec, ok, err := CumulativeSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("rolling cumulative extension was not detected")
	}
	if spec.Window.Type != "rolling" || spec.Window.Count != 3 || spec.Window.Unit != "month" {
		t.Fatalf("window = %#v", spec.Window)
	}
}

func TestCumulativeExtensionValidation(t *testing.T) {
	valid := []byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: cumulative_model
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: order_date
            datatype: Date
            expression:
              dialects:
                - {dialect: ANSI_SQL, expression: orders.order_date}
            dimension: {is_time: true}
          - name: amount
            datatype: Decimal
            expression:
              dialects:
                - {dialect: ANSI_SQL, expression: orders.amount}
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(orders.amount)"}
      - name: cumulative_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: revenue}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","count":3,"unit":"month"}}'
`)
	if _, err := NewLoader().Load(valid); err != nil {
		t.Fatalf("valid cumulative model: %v", err)
	}

	cases := []struct {
		name string
		data string
	}{
		{name: "missing base metric", data: `{"kind":"cumulative","base_metric":"missing","time_dimension":"order_date","window":{"type":"unbounded"}}`},
		{name: "rolling window missing count", data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","unit":"month"}}`},
		{name: "rolling window missing unit", data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","count":3}}`},
		{name: "rolling window unsupported unit", data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"rolling","count":3,"unit":"minute"}}`},
		{name: "unbounded window with count", data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"unbounded","count":3}}`},
		{name: "missing time dimension", data: `{"kind":"cumulative","base_metric":"revenue","time_dimension":"missing","window":{"type":"unbounded"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := []byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: cumulative_model
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: order_date
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_date}]}
            dimension: {is_time: true}
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: cumulative_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '` + tc.data + `'
`)
			_, err := NewLoader().Load(model)
			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInvalidModel {
				t.Fatalf("error = %#v, want INVALID_MODEL", err)
			}
		})
	}
}
