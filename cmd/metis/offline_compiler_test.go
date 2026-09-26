package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
)

func TestCompileDorisParameterizedSQL(t *testing.T) {
	doc := testDocument()
	limit := 100
	result, err := Compile(context.Background(), doc, "DORIS", QueryRequest{
		Metrics:    []string{"total_revenue"},
		Dimensions: []Dimension{{Name: "region"}},
		Filters:    []Filter{{Field: "region", Op: "=", Value: "APAC"}},
		Limit:      &limit,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dialect != "DORIS" {
		t.Fatalf("dialect = %q, want DORIS", result.Dialect)
	}
	for _, want := range []string{"SUM(orders.amount)", "region", "?", "LIMIT 100"} {
		if !strings.Contains(result.SQL, want) {
			t.Fatalf("SQL %q does not contain %q", result.SQL, want)
		}
	}
	if len(result.Parameters) != 1 || result.Parameters[0].Value != "APAC" {
		t.Fatalf("expected APAC binding: %#v", result.Parameters)
	}
	if len(result.OutputSchema.Columns) != 2 {
		t.Fatalf("output columns = %#v", result.OutputSchema.Columns)
	}
	if got := result.OutputSchema.Columns[0]; got.Name != "region" || got.Kind != artifact.OutputDimension || got.Datatype != ossie.DataTypeString {
		t.Fatalf("dimension output = %#v", got)
	}
	if got := result.OutputSchema.Columns[1]; got.Name != "total_revenue" || got.Kind != artifact.OutputMetric || got.Datatype != ossie.DataTypeDecimal {
		t.Fatalf("metric output = %#v", got)
	}
}

func TestCompileClickHouseParameterizedSQL(t *testing.T) {
	grain := query.TimeGrainMonth
	limit := 100
	result, err := Compile(context.Background(), clickHouseTestDocument(), "CLICKHOUSE", QueryRequest{
		Metrics:    []string{"total_revenue"},
		Dimensions: []Dimension{{Name: "order_date", Grain: &grain}},
		Filters:    []Filter{{Field: "region", Op: "=", Value: "APAC"}},
		Limit:      &limit,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SUM(orders.amount)", "FROM `sales`.`orders`", "toStartOfMonth(`orders`.`order_date`)", "?", "LIMIT 100"} {
		if !strings.Contains(result.SQL, want) {
			t.Fatalf("ClickHouse SQL %q does not contain %q", result.SQL, want)
		}
	}
	if len(result.Parameters) != 1 || result.Parameters[0].Value != "APAC" {
		t.Fatalf("expected APAC binding: %#v", result.Parameters)
	}
	if got := result.OutputSchema.Columns[0]; got.Grain == nil || *got.Grain != query.TimeGrainMonth {
		t.Fatalf("time-grain output = %#v", got)
	}
}

func TestStructuredDimensionWithGrain(t *testing.T) {
	grain := query.TimeGrainMonth
	result, err := Compile(context.Background(), testDocument(), "DORIS", QueryRequest{
		Metrics:    []string{"total_revenue"},
		Dimensions: []Dimension{{Name: "order_date", Grain: &grain}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.SQL, `DATE_TRUNC("month", `) {
		t.Fatalf("expected month time grain in SQL, got %s", result.SQL)
	}
}

func TestDimensionJSONSupportsCompactAndStructuredForms(t *testing.T) {
	var req QueryRequest
	if err := json.Unmarshal([]byte(`{"metrics":["total_revenue"],"dimensions":["region",{"name":"order_date","grain":"month"}]}`), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Dimensions) != 2 || req.Dimensions[0].Name != "region" || req.Dimensions[0].Grain != nil {
		t.Fatalf("unexpected compact dimension: %#v", req.Dimensions)
	}
	if req.Dimensions[1].Name != "order_date" || req.Dimensions[1].Grain == nil || *req.Dimensions[1].Grain != query.TimeGrainMonth {
		t.Fatalf("unexpected structured dimension: %#v", req.Dimensions[1])
	}
}

func TestCompileRejectsUnsupportedDialect(t *testing.T) {
	_, err := Compile(context.Background(), testDocument(), "SNOWFLAKE", QueryRequest{Metrics: []string{"total_revenue"}}, false)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported dialect error, got %v", err)
	}
}

func TestCompilePreservesParametersInJSON(t *testing.T) {
	value := `O'Reilly\path?`
	result, err := Compile(context.Background(), testDocument(), "DUCKDB", QueryRequest{Metrics: []string{"total_revenue"}, Filters: []Filter{{Field: "region", Op: "eq", Value: value}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := renderOutput(result)
	if err != nil {
		t.Fatal(err)
	}
	var got sql.SqlRenderResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.SQL, "?") || strings.Contains(got.SQL, value) || len(got.Parameters) != 1 || got.Parameters[0].Value != value {
		t.Fatalf("query = %#v", got)
	}
}

func testDocument() *ossie.Document {
	isTime := true
	return &ossie.Document{
		Version: ossie.SupportedSpecVersion,
		SemanticModel: []ossie.SemanticModel{{
			Name: "sales",
			Datasets: []ossie.Dataset{
				{
					Name: "orders", Source: "sales.public.orders", PrimaryKey: []string{"order_id"},
					Fields: []ossie.Field{
						{Name: "order_id", Datatype: ossie.DataTypeString, Expression: ansi("order_id")},
						{Name: "customer_id", Datatype: ossie.DataTypeString, Expression: ansi("customer_id")},
						{Name: "amount", Datatype: ossie.DataTypeDecimal, Expression: ansi("amount")},
						{Name: "order_date", Datatype: ossie.DataTypeDate, Dimension: &ossie.Dimension{IsTime: &isTime}, Expression: ansi("order_date")},
					},
				},
				{
					Name: "customer", Source: "sales.public.customer", PrimaryKey: []string{"customer_id"},
					Fields: []ossie.Field{
						{Name: "customer_id", Datatype: ossie.DataTypeString, Expression: ansi("customer_id")},
						{Name: "region", Datatype: ossie.DataTypeString, Dimension: &ossie.Dimension{}, Expression: ansi("region")},
					},
				},
			},
			Relationships: []ossie.Relationship{{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}},
			Metrics:       []ossie.Metric{{Name: "total_revenue", Datatype: ossie.DataTypeDecimal, Expression: ansi("SUM(orders.amount)")}},
		}},
	}
}

func clickHouseTestDocument() *ossie.Document {
	doc := testDocument()
	for i := range doc.SemanticModel[0].Datasets {
		switch doc.SemanticModel[0].Datasets[i].Name {
		case "orders":
			doc.SemanticModel[0].Datasets[i].Source = "sales.orders"
		case "customer":
			doc.SemanticModel[0].Datasets[i].Source = "sales.customer"
		}
	}
	return doc
}

func ansi(expr string) ossie.Expression {
	return ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: expr}}}
}
