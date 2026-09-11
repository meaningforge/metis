package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
)

const ontologySalesModel = `
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
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

const ontologyCustomerModel = `
version: "0.2.0.dev0"
name: Customer ontology
ontology:
  - concept: Customer
    description: A business customer
    type: EntityType
ontology_mappings:
  - name: customer_mapping
    concept_mappings:
      - concept: Customer
        object_mappings:
          - expression: customers.customer_id
semantic_model:
  - name: customer
    datasets:
      - name: customers
        source: sales.public.customers
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customers.customer_id}]
`

func TestLoadProjectRuntimePreservesOntologyAcrossMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeBootstrapFixture(t, filepath.Join(dir, "sales.ossie.yaml"), ontologySalesModel)
	writeBootstrapFixture(t, filepath.Join(dir, "customer.ossie.yaml"), ontologyCustomerModel)
	writeBootstrapFixture(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  models:
    path: ./*.ossie.yaml
`)

	runtime, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	projectID := filepath.Base(dir)
	project, err := runtime.SemanticManifest.Project(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Ontology) != 10 {
		t.Fatalf("ontology concepts=%d want=10 (two authored plus eight built-ins)", len(project.Ontology))
	}

	result, err := runtime.Discovery.SearchSemantics(context.Background(), service.SearchSemanticsRequest{
		Project: projectID,
		Query:   "revenue",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range result.Matches {
		if match.Kind == service.AssetOntologyConcept && match.Name == "Revenue" {
			return
		}
	}
	t.Fatalf("Revenue ontology concept not discoverable: %#v", result.Matches)
}

func TestLoadProjectRuntimeRejectsDuplicateOntologyConceptAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeBootstrapFixture(t, filepath.Join(dir, "sales.ossie.yaml"), ontologySalesModel)
	writeBootstrapFixture(t, filepath.Join(dir, "duplicate.ossie.yaml"), strings.ReplaceAll(ontologyCustomerModel, "Customer", "Revenue"))
	writeBootstrapFixture(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  models:
    path: ./*.ossie.yaml
`)

	_, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err == nil || !strings.Contains(err.Error(), `duplicate Ossie ontology concept "Revenue"`) {
		t.Fatalf("expected duplicate ontology concept error, got %v", err)
	}
}

func writeBootstrapFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
