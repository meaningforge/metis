package builder

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestValidateNonFanoutJoinAcceptsUniqueTarget(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	if err := validateNonFanoutJoin(datasets, semanticplan.Join{Relationship: rel, FromDataset: "orders", ToDataset: "customer"}); err != nil {
		t.Fatalf("safe many-to-one traversal rejected: %v", err)
	}
}

func TestAdmitMetricRelationshipJoinAcceptsInvariantAggregateWithJoinedFilterMembership(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	join := semanticplan.Join{Relationship: rel, FromDataset: "customer", ToDataset: "orders"}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	evidence, err := derivePopulationPreservationEvidence(datasets, "customer", []string{"customer"}, nil, []semanticplan.Predicate{{Dataset: "orders", Filter: query.Filter{Operator: query.FilterEQ}}}, []semanticplan.Join{join})
	if err != nil {
		t.Fatal(err)
	}
	resolved := expression.NewResolvedExpression("ANSI_SQL", "COUNT(DISTINCT customer.customer_id)").
		WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{}).
		WithAggregationProperties(expression.AggregationProperties{Function: "COUNT", Duplicate: expression.DuplicateInvariant, Derived: true})
	if err := admitMetricRelationshipJoin(join, &evidence[0], resolved); err != nil {
		t.Fatalf("proven duplicate-invariant fanout rejected: %v", err)
	}
	if evidence[0].Admission == nil || !evidence[0].Preserved() {
		t.Fatalf("fanout admission evidence = %#v", evidence[0])
	}
	if err := semanticplan.ValidatePopulationPreservationEvidence(evidence[0]); err != nil {
		t.Fatalf("admitted fanout evidence rejected by plan validation: %v", err)
	}
}

func TestAdmitMetricRelationshipJoinRejectsSensitiveAggregate(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	join := semanticplan.Join{Relationship: rel, FromDataset: "customer", ToDataset: "orders"}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	evidence, err := derivePopulationPreservationEvidence(datasets, "customer", []string{"customer"}, nil, []semanticplan.Predicate{{Dataset: "orders", Filter: query.Filter{Operator: query.FilterEQ}}}, []semanticplan.Join{join})
	if err != nil {
		t.Fatal(err)
	}
	resolved := expression.NewResolvedExpression("ANSI_SQL", "COUNT(customer.customer_id)").
		WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{}).
		WithAggregationProperties(expression.AggregationProperties{Function: "COUNT", Duplicate: expression.DuplicateSensitive, Derived: true})
	err = admitMetricRelationshipJoin(join, &evidence[0], resolved)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedRelationshipFanout {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrUnsupportedRelationshipFanout)
	}
}

func TestAdmitMetricRelationshipJoinRejectsUnderivedInvariantClaim(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	join := semanticplan.Join{Relationship: rel, FromDataset: "customer", ToDataset: "orders"}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	evidence, err := derivePopulationPreservationEvidence(datasets, "customer", []string{"customer"}, nil, []semanticplan.Predicate{{Dataset: "orders", Filter: query.Filter{Operator: query.FilterEQ}}}, []semanticplan.Join{join})
	if err != nil {
		t.Fatal(err)
	}
	resolved := expression.NewResolvedExpression("ANSI_SQL", "vendorAggregate(customer.customer_id)").
		WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{}).
		WithAggregationProperties(expression.AggregationProperties{Function: "vendorAggregate", Duplicate: expression.DuplicateInvariant})
	err = admitMetricRelationshipJoin(join, &evidence[0], resolved)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedRelationshipFanout {
		t.Fatalf("error = %#v, want fail-closed %s", err, serrors.ErrUnsupportedRelationshipFanout)
	}
}

func TestJoinedIsNullFilterDoesNotProvePopulationPreservation(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	join := semanticplan.Join{Relationship: rel, FromDataset: "customer", ToDataset: "orders"}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	evidence, err := derivePopulationPreservationEvidence(datasets, "customer", []string{"customer"}, nil, []semanticplan.Predicate{{Dataset: "orders", Filter: query.Filter{Operator: query.FilterIsNull}}}, []semanticplan.Join{join})
	if err != nil {
		t.Fatal(err)
	}
	if evidence[0].Preserved() {
		t.Fatalf("IS NULL filter incorrectly proved population preservation: %#v", evidence[0])
	}
}

func TestValidateNonFanoutJoinRejectsReverseFanout(t *testing.T) {
	rel := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	datasets := map[string]*ossie.Dataset{"orders": {Name: "orders", PrimaryKey: []string{"order_id"}}, "customer": {Name: "customer", PrimaryKey: []string{"customer_id"}}}
	err := validateNonFanoutJoin(datasets, semanticplan.Join{Relationship: rel, FromDataset: "customer", ToDataset: "orders"})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedRelationshipFanout {
		t.Fatalf("error = %v, want unsupported relationship fanout", err)
	}
}

func TestValidateNonFanoutJoinAcceptsCompositeUniqueKey(t *testing.T) {
	rel := &ossie.Relationship{Name: "fact_to_dimension", From: "fact", To: "dimension", FromColumns: []string{"tenant_id", "entity_id"}, ToColumns: []string{"tenant_id", "entity_id"}}
	datasets := map[string]*ossie.Dataset{"fact": {Name: "fact", PrimaryKey: []string{"event_id"}}, "dimension": {Name: "dimension", UniqueKeys: [][]string{{"entity_id", "tenant_id"}}}}
	if err := validateNonFanoutJoin(datasets, semanticplan.Join{Relationship: rel, FromDataset: "fact", ToDataset: "dimension"}); err != nil {
		t.Fatalf("composite unique target rejected: %v", err)
	}
}

func TestValidateNonFanoutJoinDefersTemporalCardinality(t *testing.T) {
	rel := &ossie.Relationship{Name: "events_to_history", From: "events", To: "history", FromColumns: []string{"entity_id"}, ToColumns: []string{"entity_id"}}
	datasets := map[string]*ossie.Dataset{"events": {Name: "events"}, "history": {Name: "history"}}
	temporal := &ossie.TemporalRelationshipSpec{Kind: ossie.RelationshipExtensionTemporal, Cardinality: ossie.TemporalCardinalityManyToOne}
	if err := validateNonFanoutJoin(datasets, semanticplan.Join{Relationship: rel, FromDataset: "events", ToDataset: "history", Temporal: temporal}); err != nil {
		t.Fatalf("temporal join should use temporal cardinality contract: %v", err)
	}
}
