package resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

const aggregationAlgebraModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: customer
        source: analytics.customer
        primary_key: [customer_id]
        fields:
          - name: customer_id
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: customer.customer_id}]}
      - name: orders
        source: analytics.orders
        primary_key: [order_id]
        fields:
          - name: order_id
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_id}]}
          - name: customer_id
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.customer_id}]}
          - name: status
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.status}]}
    relationships:
      - name: orders_to_customer
        from: orders
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: unique_customers
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "uniqExact(customer.customer_id)"}]}
        custom_extensions:
          - vendor_name: algebra.example
            data: '{"kind":"aggregation_algebra","version":"1","duplicate_sensitivity":"INVARIANT"}'
`

type aggregationAlgebraInterpreter struct{}

func (aggregationAlgebraInterpreter) Vendor() ossie.Vendor { return "algebra.example" }

func (aggregationAlgebraInterpreter) Requirements(location extension.Location, _ ossie.CustomExtension) ([]extension.Requirement, error) {
	evidence := aggregationAlgebraEvidence(location.Owner)
	return []extension.Requirement{evidence.Requirement()}, nil
}

func (aggregationAlgebraInterpreter) AggregationAlgebra(location extension.Location, _ ossie.CustomExtension, renderer extension.RendererContext, function string) (extension.AggregationAlgebraEvidence, bool, error) {
	if location.Scope != "metric" || renderer.Dialect != "DORIS" || function != "uniqExact" {
		return extension.AggregationAlgebraEvidence{}, false, nil
	}
	return aggregationAlgebraEvidence(location.Owner), true, nil
}

func aggregationAlgebraEvidence(metric string) extension.AggregationAlgebraEvidence {
	return extension.AggregationAlgebraEvidence{
		Identity: extension.Identity{Namespace: "algebra.example", Kind: "aggregation_algebra", Scope: "metric"},
		Metric:   metric, Version: "1", Capability: "semantic.aggregation_algebra", Function: "uniqExact",
		DuplicateSensitivity: string(expression.DuplicateInvariant), RollupAlgebra: string(expression.RollupHolistic),
		Finalize: string(expression.FinalizeNone),
	}
}

func TestRegisteredTargetCapabilityContributesAggregationAlgebraAndAdmitsProvenFanout(t *testing.T) {
	store := aggregationAlgebraStore(t)
	inventory := extension.NewInventory()
	if err := inventory.Register(aggregationAlgebraInterpreter{}); err != nil {
		t.Fatal(err)
	}
	capabilities := extension.NewRegistry()
	evidence := aggregationAlgebraEvidence("unique_customers")
	if err := capabilities.Register(extension.Registration{
		Identity: evidence.Identity, Version: evidence.Version, Capability: evidence.Capability,
		Renderer: extension.RendererContext{Dialect: "DORIS"},
	}); err != nil {
		t.Fatal(err)
	}

	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(store).WithExtensionCapabilities(inventory, capabilities).ResolveForRenderer(
		context.Background(), aggregationAlgebraQuery(), renderer,
	)
	if err != nil {
		t.Fatal(err)
	}
	properties := resolved.Metrics[0].Expression.AggregationPropertyValues()
	if len(properties) != 1 || !properties[0].Derived || properties[0].Duplicate != expression.DuplicateInvariant {
		t.Fatalf("aggregation properties = %#v, want registered invariant evidence", properties)
	}
	resolvedEvidence := resolved.Metrics[0].Expression.ExtensionEvidenceValues()
	if len(resolvedEvidence) != 1 {
		t.Fatalf("extension evidence = %#v, want one registered algebra claim", resolvedEvidence)
	}
	if _, ok := resolvedEvidence[0].(extension.AggregationAlgebraEvidence); !ok {
		t.Fatalf("extension evidence type = %T", resolvedEvidence[0])
	}
	if _, err := planner.New().Plan(context.Background(), resolved, renderer); err != nil {
		t.Fatalf("registered invariant aggregation rejected despite population proof: %v", err)
	}
}

func TestOpaqueModelAuthoredAlgebraClaimCannotUnlockFanout(t *testing.T) {
	renderer := mustRenderer(t, "DORIS")
	resolved, err := resolver.New(aggregationAlgebraStore(t)).ResolveForRenderer(
		context.Background(), aggregationAlgebraQuery(), renderer,
	)
	if err != nil {
		t.Fatal(err)
	}
	properties := resolved.Metrics[0].Expression.AggregationPropertyValues()
	if len(properties) != 1 || properties[0].Derived || properties[0].Duplicate != expression.DuplicateUnknown {
		t.Fatalf("opaque model-authored claim changed algebra: %#v", properties)
	}
	if evidence := resolved.Metrics[0].Expression.ExtensionEvidenceValues(); len(evidence) != 0 {
		t.Fatalf("opaque payload became typed evidence: %#v", evidence)
	}
	_, err = planner.New().Plan(context.Background(), resolved, renderer)
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrUnsupportedRelationshipFanout {
		t.Fatalf("plan error = %v, want %s", err, serrors.ErrUnsupportedRelationshipFanout)
	}
}

func TestRegisteredInterpreterWithoutSelectedTargetCapabilityRemainsUnknown(t *testing.T) {
	inventory := extension.NewInventory()
	if err := inventory.Register(aggregationAlgebraInterpreter{}); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.New(aggregationAlgebraStore(t)).WithExtensionCapabilities(inventory, extension.NewRegistry()).ResolveForRenderer(
		context.Background(), aggregationAlgebraQuery(), mustRenderer(t, "DORIS"),
	)
	if err != nil {
		t.Fatal(err)
	}
	properties := resolved.Metrics[0].Expression.AggregationPropertyValues()
	if len(properties) != 1 || properties[0].Derived || properties[0].Duplicate != expression.DuplicateUnknown {
		t.Fatalf("unsupported target capability changed algebra: %#v", properties)
	}
}

func aggregationAlgebraStore(t *testing.T) *manifest.Store {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(aggregationAlgebraModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("demo", doc)
	if err != nil {
		t.Fatal(err)
	}
	return manifest.NewStore(snapshot)
}

func aggregationAlgebraQuery() query.SemanticQuery {
	return query.SemanticQuery{
		Project: "demo", Model: "sales", Metrics: []query.MetricRef{{Name: "unique_customers"}},
		Filters: []query.Filter{{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"}},
	}
}
