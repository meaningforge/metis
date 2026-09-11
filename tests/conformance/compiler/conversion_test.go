package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const conversionCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: conversion
    datasets:
      - name: signups
        source: analytics.signups
        primary_key: [signup_id]
        fields:
          - {name: signup_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: signup_id}]}}
          - {name: signup_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: signup_time}]}, dimension: {is_time: true}}
          - {name: user_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: user_id}]}}
          - {name: campaign, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: campaign}]}, dimension: {}}
          - {name: signup_value, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: signup_value}]}}
      - name: purchases
        source: analytics.purchases
        primary_key: [purchase_id]
        fields:
          - {name: purchase_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: purchase_id}]}}
          - {name: purchase_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: purchase_time}]}, dimension: {is_time: true}}
          - {name: purchaser_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: purchaser_id}]}}
          - {name: purchase_value, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: purchase_value}]}}
    metrics:
      - name: signup_events
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(signups.signup_value)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"signup_time"}'
      - name: purchase_events
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(purchases.purchase_value)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"purchase_time"}'
      - name: signup_to_purchase_rate
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "purchase_events / signup_events"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"conversion","base_metric":"signup_events","conversion_metric":"purchase_events","entity":{"base_property":"user_id","conversion_property":"purchaser_id"},"calculation":"conversion_rate","window":{"count":7,"unit":"day"}}'
      - name: signup_conversions
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: purchase_events}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"conversion","base_metric":"signup_events","conversion_metric":"purchase_events","entity":{"base_property":"user_id","conversion_property":"purchaser_id"},"calculation":"conversions","window":{"count":7,"unit":"day"}}'
`

func TestConversionCompilerConformance(t *testing.T) {
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "signup_to_purchase_rate"}},
		Dimensions: []query.DimensionRef{{Name: "campaign"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(conversionCompilerModel), "conversion", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(sqlQuery.SQL)
			upper := strings.ToUpper(sqlQuery.SQL)
			for _, want := range []string{
				"__metis_conversion_base_population",
				"__metis_conversion_candidates",
				"__metis_conversion_assigned",
				"ROW_NUMBER() OVER (",
				"PARTITION BY",
				"ORDER BY",
				"FULL OUTER JOIN",
				"NULLIF",
			} {
				if !strings.Contains(upper, strings.ToUpper(want)) {
					t.Fatalf("%s conversion SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			for _, want := range []string{"signup_time", "signup_id"} {
				if !strings.Contains(lower, want) {
					t.Fatalf("%s conversion SQL lost deterministic assignment key %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			if strings.Count(upper, " DESC") < 2 {
				t.Fatalf("%s conversion SQL lost deterministic descending assignment order:\n%s", dialect, sqlQuery.SQL)
			}

			baseAt := strings.Index(lower, "__metis_conversion_base_population")
			candidateAt := strings.Index(lower, "__metis_conversion_candidates")
			if baseAt < 0 || candidateAt < 0 || candidateAt <= baseAt {
				t.Fatalf("%s conversion SQL has invalid base/candidate stage ordering:\n%s", dialect, sqlQuery.SQL)
			}
			baseStage := lower[baseAt:candidateAt]
			if !strings.Contains(baseStage, "signups") || strings.Contains(baseStage, "purchases") {
				t.Fatalf("%s base denominator is not independent from conversion candidates:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}

func TestFilteredConversionCompilerPreservesBasePopulationOwnership(t *testing.T) {
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "signup_to_purchase_rate"}},
		Dimensions: []query.DimensionRef{{Name: "campaign"}},
		Filters:    []query.Filter{{Field: "campaign", Operator: query.FilterEQ, Value: "paid"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		sqlQuery, err := compileModel(t, []byte(conversionCompilerModel), "conversion", query, dialect)
		if err != nil {
			t.Fatalf("%s: %v", dialect, err)
		}
		lower := strings.ToLower(sqlQuery.SQL)
		if strings.Count(sqlQuery.SQL, "?") < 2 {
			t.Fatalf("%s conversion filter was not applied to both base population and candidate base side:\n%s", dialect, sqlQuery.SQL)
		}
		paidParameters := 0
		for _, parameter := range sqlQuery.Parameters {
			if parameter.Value == "paid" {
				paidParameters++
			}
		}
		if paidParameters != 2 {
			t.Fatalf("%s conversion filter parameters = %#v, want two base-owned paid bindings", dialect, sqlQuery.Parameters)
		}
		if !strings.Contains(lower, "__metis_conversion_base_population") || !strings.Contains(lower, "__metis_conversion_candidates") {
			t.Fatalf("%s filtered conversion lost fan-out-safe stages:\n%s", dialect, sqlQuery.SQL)
		}
	}
}

func TestConversionCountCompilerUsesAssignedNumerator(t *testing.T) {
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "signup_conversions"}},
		Dimensions: []query.DimensionRef{{Name: "campaign"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		sqlQuery, err := compileModel(t, []byte(conversionCompilerModel), "conversion", query, dialect)
		if err != nil {
			t.Fatalf("%s: %v", dialect, err)
		}
		upper := strings.ToUpper(sqlQuery.SQL)
		if !strings.Contains(upper, "__METIS_CONVERSION_ASSIGNED") || !strings.Contains(upper, "__METIS_ASSIGNED_VALUE") {
			t.Fatalf("%s conversion count did not use the rank-1 assigned numerator:\n%s", dialect, sqlQuery.SQL)
		}
		if strings.Contains(upper, "NULLIF") {
			t.Fatalf("%s conversions unexpectedly lowered as conversion_rate:\n%s", dialect, sqlQuery.SQL)
		}
	}
}
