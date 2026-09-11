package fixture

const (
	AnalyticsProject = "analytics"
	AnalyticsModel   = "workflow"
)

// AnalyticsModelYAML is the shared semantic contract for the production
// attribute_metric and compare_metrics real-engine workflow contract.
const AnalyticsModelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: workflow
    datasets:
      - name: events
        source: analytics.workflow_events
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.amount}]}
          - name: event_time
            datatype: DateTime
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.event_time}]}
            dimension: {is_time: true}
          - name: region
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.region}]}
            dimension: {}
          - name: converted
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.converted}]}
          - name: sessions
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.sessions}]}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(events.amount)}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: converted
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(events.converted)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: sessions
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(events.sessions)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: conversion_rate
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "converted / sessions"}]}
`
