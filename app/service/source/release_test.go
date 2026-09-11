package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
)

const releaseSalesModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    ai_context:
      team: finance
      arbitrary_nested:
        retained: true
    datasets:
      - name: orders
        source: sales.public.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.region}]
            dimension: {}
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
        custom_extensions:
          - vendor_name: example.vendor
            data: '{"unknown":true,"nested":{"value":7}}'
`

const releaseCustomersModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: customers
    datasets:
      - name: customers
        source: sales.public.customers
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customers.customer_id}]
    metrics:
      - name: customer_count
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: COUNT(customers.customer_id)}]
`

func TestLoadProjectIsDeterministicAndProjectScoped(t *testing.T) {
	first := writeProject(t, "first", map[string]string{"sales.ossie.yaml": releaseSalesModel, "customers.ossie.yaml": releaseCustomersModel}, `
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
  customers:
    path: ./customers.ossie.yaml
`)
	second := writeProject(t, "second", map[string]string{"sales.ossie.yaml": releaseSalesModel, "customers.ossie.yaml": releaseCustomersModel}, `
semantic_sources:
  customers:
    path: ./customers.ossie.yaml
  sales:
    path: ./sales.ossie.yaml
`)
	left, err := LoadProject("finance", first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := LoadProject("finance", second)
	if err != nil {
		t.Fatal(err)
	}
	if left.Bundle.ContentDigest != right.Bundle.ContentDigest || left.Manifest.Digest != right.Manifest.Digest {
		t.Fatalf("equivalent projects are unstable: bundle %s != %s, manifest %s != %s", left.Bundle.ContentDigest, right.Bundle.ContentDigest, left.Manifest.Digest, right.Manifest.Digest)
	}
	if got := []string{left.Bundle.Documents[0].Source, left.Bundle.Documents[1].Source}; !reflect.DeepEqual(got, []string{"customers", "sales"}) {
		t.Fatalf("document order = %v", got)
	}
	other, err := LoadProject("marketing", first)
	if err != nil {
		t.Fatal(err)
	}
	if other.Bundle.ContentDigest != left.Bundle.ContentDigest {
		t.Fatalf("content digest unexpectedly depends on project: %s != %s", other.Bundle.ContentDigest, left.Bundle.ContentDigest)
	}
	if other.Manifest.Digest == left.Manifest.Digest {
		t.Fatal("project-scoped semantic manifests share an identity")
	}
}

func TestFormatDocumentIsStableAndPreservesUnknownExtensionData(t *testing.T) {
	first, err := FormatDocument([]byte(releaseSalesModel))
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatDocument(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("formatting is not idempotent:\n%s\n---\n%s", first, second)
	}
	if !strings.HasSuffix(string(first), "\n") || strings.HasSuffix(string(first), "\n\n") {
		t.Fatalf("formatted document must have exactly one trailing newline: %q", first[len(first)-2:])
	}
	document, err := ossie.NewLoader().Load(first)
	if err != nil {
		t.Fatal(err)
	}
	extension := document.SemanticModel[0].Metrics[0].CustomExtensions[0]
	if extension.VendorName != "example.vendor" || extension.Data != `{"unknown":true,"nested":{"value":7}}` {
		t.Fatalf("unknown extension changed: %#v", extension)
	}
	context, ok := document.SemanticModel[0].AIContext.(map[string]any)
	if !ok || context["team"] != "finance" {
		t.Fatalf("AI context was not preserved: %#v", document.SemanticModel[0].AIContext)
	}
}

func TestCompareClassifiesCanonicalSemanticChangesDeterministically(t *testing.T) {
	basePath := writeProject(t, "base", map[string]string{"model.ossie.yaml": releaseSalesModel}, projectFor("model.ossie.yaml"))
	changed := strings.Replace(releaseSalesModel, "SUM(orders.amount)", "SUM(orders.amount) * 2", 1)
	changed = strings.Replace(changed, "    metrics:\n", "    metrics:\n      - name: order_count\n        datatype: Integer\n        expression:\n          dialects: [{dialect: ANSI_SQL, expression: COUNT(orders.amount)}]\n", 1)
	candidatePath := writeProject(t, "candidate", map[string]string{"model.ossie.yaml": changed}, projectFor("model.ossie.yaml"))
	base, err := LoadProject("finance", basePath)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := LoadProject("finance", candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Compare(base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Changes) == 0 {
		t.Fatal("semantic change was not detected")
	}
	var foundAdded, foundModified bool
	for index, change := range diff.Changes {
		if index > 0 && diff.Changes[index-1].Reference > change.Reference {
			t.Fatalf("changes are not ordered: %#v", diff.Changes)
		}
		foundAdded = foundAdded || change.Type == ChangeAdded && change.Reference == "model/sales/metric/order_count"
		foundModified = foundModified || change.Type == ChangeModified && change.Reference == "model/sales/metric/revenue"
	}
	if !foundAdded || !foundModified {
		t.Fatalf("expected metric changes, got %#v", diff.Changes)
	}
}

func TestCompareIncludesGovernanceMetadataChanges(t *testing.T) {
	basePath := writeProject(t, "governance-base", map[string]string{"model.ossie.yaml": releaseSalesModel}, projectFor("model.ossie.yaml"))
	governed := strings.Replace(releaseSalesModel,
		"        custom_extensions:\n          - vendor_name: example.vendor",
		"        custom_extensions:\n          - vendor_name: METIS\n            data: '{\"kind\":\"asset_governance\",\"owner\":\"team:finance\",\"certification\":\"certified\"}'\n          - vendor_name: example.vendor", 1)
	candidatePath := writeProject(t, "governance-candidate", map[string]string{"model.ossie.yaml": governed}, projectFor("model.ossie.yaml"))
	base, err := LoadProject("finance", basePath)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := LoadProject("finance", candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Compare(base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range diff.Changes {
		if change.Type == ChangeModified && change.AssetKind == "metric" && change.Reference == "model/sales/metric/revenue" {
			return
		}
	}
	t.Fatalf("governance metric change missing: %#v", diff.Changes)
}

func writeProject(t *testing.T, name string, files map[string]string, project string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, "project.yaml")
	if err := os.WriteFile(config, []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	return config
}

func projectFor(path string) string {
	return "semantic_sources:\n  model:\n    path: ./" + path + "\n"
}

func readJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, value); err != nil {
		t.Fatal(err)
	}
}
