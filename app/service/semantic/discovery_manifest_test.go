package semantic

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const modelYAML = `
version: "0.2.0.dev0"
name: Sales ontology
ontology:
  - concept: Revenue
    description: Business revenue amount
    type: ValueType
    extends: [Decimal]
ontology_mappings:
  - name: sales_mapping
    concept_mappings:
      - concept: Revenue
        object_mappings:
          - expression: orders.amount
semantic_model:
  - name: sales
    description: Sales and customer revenue semantics
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.customer_id}]
      - name: customer
        source: analytics.customer
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.customer_id}]
          - name: region
            description: Customer sales region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.region}]
            dimension: {}
      - name: costs
        source: analytics.costs
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.amount}]
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.customer_id}]
      - name: inventory
        source: analytics.inventory
        fields:
          - name: category
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: inventory.category}]
            dimension: {}
    relationships:
      - name: orders_to_customer
        from: orders
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
      - name: costs_to_customer
        from: costs
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: total_revenue
        description: Total recognized revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
      - name: total_cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(costs.amount)}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: total_revenue - total_cost}]
      - name: unsafe_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount) - SUM(costs.amount)"}]
`

func loadDoc(t *testing.T) *ossie.Document {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func newService(t *testing.T) *DiscoveryService {
	t.Helper()
	snapshot, err := manifest.BuildProjectManifest("finance", loadDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	return NewDiscoveryService(manifest.NewStore(snapshot)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
}

func TestSearchFindsMetricByNaturalTokens(t *testing.T) {
	result, err := newService(t).search(context.Background(), "finance", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range result.Matches {
		if m.Kind == AssetMetric && m.Name == "total_revenue" && m.Model == "sales" && m.Project == "finance" {
			found = true
		}
	}
	if !found {
		t.Fatalf("total_revenue not found: %#v", result.Matches)
	}
}
func TestSearchFindsDimensionByDescription(t *testing.T) {
	result, err := newService(t).search(context.Background(), "finance", "sales region")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range result.Matches {
		if m.Kind == AssetDimension && m.Qualified == "customer.region" {
			found = true
		}
	}
	if !found {
		t.Fatalf("customer.region not found: %#v", result.Matches)
	}
}
func TestSearchFindsOntologyConceptWithoutMappings(t *testing.T) {
	result, err := newService(t).search(context.Background(), "finance", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range result.Matches {
		if m.Kind != AssetOntologyConcept || m.Name != "Revenue" {
			continue
		}
		if m.OntologyType != "ValueType" {
			t.Fatalf("ontology type=%q", m.OntologyType)
		}
		if len(m.Extends) != 1 || m.Extends[0] != "Decimal" {
			t.Fatalf("extends=%v", m.Extends)
		}
		return
	}
	t.Fatalf("ontology Revenue not found: %#v", result.Matches)
}
func TestSearchCannotReachConceptThroughMappedExpression(t *testing.T) {
	result, err := newService(t).search(context.Background(), "finance", "orders")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range result.Matches {
		if m.Kind == AssetOntologyConcept && m.Name == "Revenue" {
			t.Fatalf("mapping leaked through search: %#v", m)
		}
	}
}
func TestLookupAPIsReturnCanonicalOssieObjects(t *testing.T) {
	svc, ctx := newService(t), context.Background()
	model, err := svc.getModel(ctx, "finance", "sales")
	if err != nil || model.Name != "sales" {
		t.Fatalf("unexpected model: %#v err=%v", model, err)
	}
	metric, err := svc.getMetric(ctx, "finance", "sales", "total_revenue")
	if err != nil || metric.Name != "total_revenue" {
		t.Fatalf("unexpected metric: %#v err=%v", metric, err)
	}
	dim, err := svc.getDimension(ctx, "finance", "sales", "region")
	if err != nil || dim.Dataset != "customer" {
		t.Fatalf("unexpected dimension: %#v err=%v", dim, err)
	}
	rels, err := svc.getRelationships(ctx, "finance", "sales")
	if err != nil || len(rels) != 2 || rels[0].Name != "costs_to_customer" || rels[1].Name != "orders_to_customer" {
		t.Fatalf("unexpected relationships: %#v err=%v", rels, err)
	}
}

func TestMetricDimensionCompatibilityAcrossDerivedMetricSources(t *testing.T) {
	result, err := newService(t).getMetricDimensions(context.Background(), "finance", "sales", "gross_margin")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.SourceDatasets, []string{"costs", "orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source datasets = %v, want %v", got, want)
	}
	byName := map[string]DimensionCompatibility{}
	for _, dimension := range result.Dimensions {
		byName[dimension.Qualified] = dimension
	}
	region := byName["customer.region"]
	if region.Status != CompatibilityCompatible || len(region.Paths) != 2 {
		t.Fatalf("region compatibility = %#v", region)
	}
	if got := region.Paths[0].Relationships; !reflect.DeepEqual(got, []string{"costs_to_customer"}) {
		t.Fatalf("cost path = %v", got)
	}
	category := byName["inventory.category"]
	if category.Status != CompatibilityUnreachable || len(category.Issues) != 2 {
		t.Fatalf("category compatibility = %#v", category)
	}
}

func TestMetricDimensionCompatibilityRejectsAmbiguousLeafSource(t *testing.T) {
	_, err := newService(t).getMetricDimensions(context.Background(), "finance", "sales", "unsafe_margin")
	if err == nil {
		t.Fatal("expected ambiguous metric source")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrAmbiguousMetricSource {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrAmbiguousMetricSource)
	}
}

func TestMetricDimensionCompatibilitySurfacesAmbiguousPaths(t *testing.T) {
	doc := loadDoc(t)
	model := &doc.SemanticModel[0]
	duplicateRoute := model.Relationships[0]
	duplicateRoute.Name = "orders_to_customer_alternate"
	model.Relationships = append(model.Relationships, duplicateRoute)
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewDiscoveryService(manifest.NewStore(snapshot)).getMetricDimensions(context.Background(), "finance", "sales", "total_revenue")
	if err != nil {
		t.Fatal(err)
	}
	for _, dimension := range result.Dimensions {
		if dimension.Qualified != "customer.region" {
			continue
		}
		if dimension.Status != CompatibilityAmbiguous || len(dimension.Issues) != 1 || len(dimension.Issues[0].RelationshipPaths) != 2 {
			t.Fatalf("ambiguous compatibility = %#v", dimension)
		}
		return
	}
	t.Fatal("customer.region compatibility was not returned")
}
func TestProjectsIsolateSameNamedModel(t *testing.T) {
	finance, err := manifest.BuildProjectManifest("finance", loadDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	growth, err := manifest.BuildProjectManifest("growth", loadDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := manifest.MergeManifests(finance, growth)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewDiscoveryService(manifest.NewStore(merged)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	f, err := svc.search(context.Background(), "finance", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	g, err := svc.search(context.Background(), "growth", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Matches) == 0 || len(g.Matches) == 0 || f.Matches[0].Project == g.Matches[0].Project {
		t.Fatalf("project isolation failed: finance=%#v growth=%#v", f.Matches, g.Matches)
	}
}
