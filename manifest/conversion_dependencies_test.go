package manifest_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

func TestConversionExtensionOwnsMetricDependencies(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: commerce
    datasets:
      - name: events
        source: analytics.events
        fields:
          - name: event_time
            datatype: DateTime
            dimension: {is_time: true}
            expression:
              dialects: [{dialect: ANSI_SQL, expression: event_time}]
          - name: user_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: user_id}]
          - name: signup_flag
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: signup_flag}]
          - name: purchase_flag
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: purchase_flag}]
    metrics:
      - name: signup_events
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.signup_flag)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"event_time"}'
      - name: purchase_events
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.purchase_flag)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"event_time"}'
      - name: signup_to_purchase_rate
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "1"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"conversion","base_metric":"signup_events","conversion_metric":"purchase_events","entity":{"base_property":"user_id","conversion_property":"user_id"},"calculation":"conversion_rate","window":{"count":7,"unit":"day"}}'
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, err := snapshot.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := project.Model("commerce")
	if err != nil {
		t.Fatal(err)
	}
	dependency, ok := model.MetricDependency("signup_to_purchase_rate")
	if !ok {
		t.Fatal("conversion dependency not indexed")
	}
	wantDependencies := []string{"purchase_events", "signup_events"}
	if !reflect.DeepEqual(dependency.Metrics, wantDependencies) {
		t.Fatalf("conversion dependencies = %#v, want %#v", dependency.Metrics, wantDependencies)
	}
	if len(dependency.DirectDatasets) != 0 {
		t.Fatalf("conversion owner expression unexpectedly owns source datasets: %#v", dependency.DirectDatasets)
	}
	order, err := model.MetricDependencyGraph.EvaluationOrder("signup_to_purchase_rate")
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"purchase_events", "signup_events", "signup_to_purchase_rate"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("evaluation order = %#v, want %#v", order, wantOrder)
	}
}

func TestConversionDependenciesDeduplicateExpressionReferences(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: commerce
    datasets:
      - name: events
        source: analytics.events
        fields:
          - name: event_time
            datatype: DateTime
            dimension: {is_time: true}
            expression:
              dialects: [{dialect: ANSI_SQL, expression: event_time}]
          - name: user_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: user_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
    metrics:
      - name: base_events
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.amount)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"event_time"}'
      - name: conversion_events
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.amount)"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"event_time"}'
      - name: conversions
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "base_events + conversion_events"}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"conversion","base_metric":"base_events","conversion_metric":"conversion_events","entity":{"base_property":"user_id","conversion_property":"user_id"},"calculation":"conversions"}'
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("commerce")
	dependency, _ := model.MetricDependency("conversions")
	want := []string{"base_events", "conversion_events"}
	if !reflect.DeepEqual(dependency.Metrics, want) {
		t.Fatalf("dependencies = %#v, want %#v", dependency.Metrics, want)
	}
}
