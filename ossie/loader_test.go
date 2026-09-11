package ossie_test

import (
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

const model = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: order_id
            datatype: String
            expression:
              dialects:
                - dialect: ANSI_SQL
                  expression: order_id
          - name: amount
            datatype: Decimal
            expression:
              dialects:
                - dialect: ANSI_SQL
                  expression: amount
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects:
            - dialect: ANSI_SQL
              expression: SUM(orders.amount)
    custom_extensions:
      - vendor_name: METIS_TEST
        data: '{"preserve":true}'
`

func TestLoadAndBuildSnapshot(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.SemanticModel[0].CustomExtensions[0].VendorName; got != "METIS_TEST" {
		t.Fatalf("extension not preserved: %q", got)
	}
	if got := doc.SemanticModel[0].CustomExtensions[0].Data; got != `{"preserve":true}` {
		t.Fatalf("extension payload not preserved: %q", got)
	}

	snapshot, err := manifest.BuildProjectManifest("loader-test", doc)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest == "" {
		t.Fatal("expected digest")
	}
	project, err := snapshot.Project("loader-test")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := project.Model("sales")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.Metrics["total_revenue"]; !ok {
		t.Fatal("metric was not indexed")
	}
}

func TestLoadOntologyEnvelope(t *testing.T) {
	ontology := `
version: "0.2.0.dev0"
name: Flights
description: Ontology of flights
requires:
  - COUNT[Airport] > 0
ontology:
  - concept: Airport
    type: Entity
    requires: [Airport.code != '']
ontology_mappings:
  - name: flights_mapping
    semantic_model:
      name: Flights semantic model
`
	doc, err := ossie.NewLoader().Load([]byte(ontology))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "Flights" {
		t.Fatalf("unexpected ontology name: %q", doc.Name)
	}
	if len(doc.Requires) != 1 || len(doc.Ontology) != 1 || len(doc.OntologyMappings) != 1 {
		t.Fatalf("ontology envelope was not preserved: %#v", doc)
	}
	if got := doc.Ontology[0]["concept"]; got != "Airport" {
		t.Fatalf("unexpected concept: %#v", got)
	}
	if got := doc.OntologyMappings[0]["name"]; got != "flights_mapping" {
		t.Fatalf("unexpected mapping: %#v", got)
	}
}

func TestRejectBrokenRelationship(t *testing.T) {
	broken := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
    relationships:
      - name: bad
        from: orders
        to: missing
        from_columns: [customer_id]
        to_columns: [id]
`
	if _, err := ossie.NewLoader().Load([]byte(broken)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRejectDuplicateRelationshipName(t *testing.T) {
	duplicate := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - {name: orders, source: sales.orders}
      - {name: customer, source: sales.customer}
    relationships:
      - {name: orders_to_customer, from: orders, to: customer, from_columns: [customer_id], to_columns: [id]}
      - {name: orders_to_customer, from: orders, to: customer, from_columns: [customer_id], to_columns: [id]}
`
	if _, err := ossie.NewLoader().Load([]byte(duplicate)); err == nil {
		t.Fatal("expected duplicate relationship validation error")
	}
}

func TestRejectUnknownOssieProperty(t *testing.T) {
	unknown := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    unsupported_metis_field: true
    datasets:
      - name: orders
        source: sales.public.orders
`
	if _, err := ossie.NewLoader().Load([]byte(unknown)); err == nil {
		t.Fatal("expected unknown schema property to be rejected")
	}
}

func TestAIContextAllowsExtensionProperties(t *testing.T) {
	withContext := `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    ai_context:
      instructions: use revenue for sales questions
      organization_term: GMV
    datasets:
      - name: orders
        source: sales.public.orders
`
	if _, err := ossie.NewLoader().Load([]byte(withContext)); err != nil {
		t.Fatalf("AIContext additional properties should be accepted: %v", err)
	}
}

func TestRejectUnsupportedSpecVersion(t *testing.T) {
	wrongVersion := `
version: "9.9.9"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.public.orders
`
	if _, err := ossie.NewLoader().Load([]byte(wrongVersion)); err == nil {
		t.Fatal("expected unsupported Ossie version to be rejected")
	}
}
