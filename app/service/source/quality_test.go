package source

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestQualityReportJSONContractUsesTypedEnumsAndLocation(t *testing.T) {
	report := QualityReport{
		SchemaVersion: QualitySchemaVersion, ProjectID: "finance", ContentDigest: "sha256:content",
		PublicationThreshold: SeverityWarning, Publishable: false,
		Diagnostics: []Diagnostic{{
			Code: DiagnosticAliasConflict, Severity: SeverityWarning, Asset: "metric:sales.revenue",
			RelatedAssets: []string{"metric:growth.revenue"},
			Location:      &DiagnosticLocation{Path: "models/sales.yaml", Line: 7, Column: 3},
			Message:       "alias conflict", CallerAction: serrors.CallerActionChangeModel,
			Evidence: &DiagnosticEvidence{Reason: ReasonGovernedAlias, NormalizedIdentity: "gmv"},
		}},
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"project_id":"finance","content_digest":"sha256:content","publication_threshold":"warning","publishable":false,"diagnostics":[{"code":"MODEL_QUALITY_ALIAS_CONFLICT","severity":"warning","asset":"metric:sales.revenue","related_assets":["metric:growth.revenue"],"location":{"path":"models/sales.yaml","line":7,"column":3},"message":"alias conflict","caller_action":"CHANGE_MODEL","evidence":{"reason":"governed_alias","normalized_identity":"gmv"}}]}`
	if string(body) != want {
		t.Fatalf("quality report JSON contract:\n got %s\nwant %s", body, want)
	}
}

func TestEquivalentMetricDiagnosticsAreDeterministicAndAdvisoryByDefault(t *testing.T) {
	firstConfig := writeProject(t, "first", map[string]string{"model.ossie.yaml": qualityEquivalentModel}, projectFor("model.ossie.yaml"))
	secondConfig := writeProject(t, "second", map[string]string{"model.ossie.yaml": qualityEquivalentModel}, projectFor("model.ossie.yaml"))
	first, err := LoadProject("finance", firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadProject("finance", secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Quality.Publishable || first.Quality.PublicationThreshold != SeverityError {
		t.Fatalf("default quality report = %#v", first.Quality)
	}
	if len(first.Quality.Diagnostics) != 1 {
		t.Fatalf("quality diagnostics = %#v", first.Quality.Diagnostics)
	}
	diagnostic := first.Quality.Diagnostics[0]
	if diagnostic.Code != DiagnosticEquivalentMetricDefinition || diagnostic.Severity != SeverityWarning || diagnostic.Asset != "metric:retail.sales_by_brand" {
		t.Fatalf("equivalence diagnostic = %#v", diagnostic)
	}
	if diagnostic.Evidence == nil || diagnostic.Evidence.Reason != ReasonExactDefinition {
		t.Fatalf("equivalence reason = %#v", diagnostic.Evidence)
	}
	if !reflect.DeepEqual(diagnostic.RelatedAssets, []string{"metric:retail.total_sales"}) || diagnostic.Location == nil || diagnostic.Location.Path != "model.ossie.yaml" {
		t.Fatalf("equivalence references = %#v", diagnostic)
	}
	if extensions := first.Document.SemanticModel[0].Metrics[0].CustomExtensions; len(extensions) != 1 || extensions[0].VendorName != "example.vendor" {
		t.Fatalf("opaque metric extension was not preserved: %#v", extensions)
	}
	left, _ := json.Marshal(first.Quality)
	right, _ := json.Marshal(second.Quality)
	if string(left) != string(right) {
		t.Fatalf("quality output is not byte-stable:\n%s\n%s", left, right)
	}

}

func TestGovernanceQualityDiagnosticsUseTypedEvidence(t *testing.T) {
	model := strings.Replace(releaseSalesModel,
		"        custom_extensions:\n          - vendor_name: example.vendor",
		"        custom_extensions:\n          - vendor_name: METIS\n            data: '{\"kind\":\"asset_governance\",\"lifecycle\":\"deprecated\",\"certification\":\"certified\"}'\n          - vendor_name: example.vendor", 1)
	config := writeProject(t, "governance-quality", map[string]string{"model.ossie.yaml": model}, projectFor("model.ossie.yaml"))
	candidate, err := LoadProject("finance", config)
	if err != nil {
		t.Fatal(err)
	}
	var deprecated, owner bool
	for _, diagnostic := range candidate.Quality.Diagnostics {
		if diagnostic.Asset != "metric:sales.revenue" || diagnostic.Evidence == nil {
			continue
		}
		switch diagnostic.Code {
		case DiagnosticDeprecatedWithoutReplacement:
			deprecated = diagnostic.Evidence.Reason == ReasonDeprecatedWithoutReplacement && diagnostic.Evidence.Lifecycle == "deprecated"
		case DiagnosticCertifiedWithoutOwner:
			owner = diagnostic.Evidence.Reason == ReasonCertifiedWithoutOwner && diagnostic.Evidence.Certification == "certified"
		}
	}
	if !deprecated || !owner || !candidate.Quality.Publishable {
		t.Fatalf("governance diagnostics = %#v", candidate.Quality)
	}
}

func TestAliasAndIdentityCollisionsIncludeAllMultiDocumentReferences(t *testing.T) {
	config := writeProject(t, "aliases", map[string]string{
		"finance.ossie.yaml": strings.ReplaceAll(qualityAliasModel, "MODEL_NAME", "finance_model"),
		"growth.ossie.yaml":  strings.ReplaceAll(qualityAliasModel, "MODEL_NAME", "growth_model"),
	}, `
semantic_sources:
  finance:
    path: ./finance.ossie.yaml
  growth:
    path: ./growth.ossie.yaml
`)
	candidate, err := LoadProject("analytics", config)
	if err != nil {
		t.Fatal(err)
	}
	var alias *Diagnostic
	for index := range candidate.Quality.Diagnostics {
		if candidate.Quality.Diagnostics[index].Code == DiagnosticAliasConflict {
			alias = &candidate.Quality.Diagnostics[index]
		}
	}
	if alias == nil {
		t.Fatalf("alias conflict missing: %#v", candidate.Quality.Diagnostics)
	}
	refs := append([]string{alias.Asset}, alias.RelatedAssets...)
	want := []string{"metric:finance_model.revenue", "metric:growth_model.revenue"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("alias conflict refs = %v, want %v", refs, want)
	}
	if alias.Evidence == nil {
		t.Fatalf("alias evidence missing: %#v", alias)
	}
	locations := alias.Evidence.SourceLocations
	if locations[want[0]].Path != "finance.ossie.yaml" || locations[want[1]].Path != "growth.ossie.yaml" {
		t.Fatalf("alias source locations = %#v", locations)
	}
	var identity *Diagnostic
	for index := range candidate.Quality.Diagnostics {
		if candidate.Quality.Diagnostics[index].Code == DiagnosticDiscoveryIdentityCollision {
			identity = &candidate.Quality.Diagnostics[index]
		}
	}
	if identity == nil || len(identity.RelatedAssets) != 1 {
		t.Fatalf("canonical-name collision missing: %#v", candidate.Quality.Diagnostics)
	}
}

func TestQualityRulesIgnoreDescriptionsAndAIContext(t *testing.T) {
	config := writeProject(t, "prose", map[string]string{"model.ossie.yaml": qualityProseModel}, projectFor("model.ossie.yaml"))
	candidate, err := LoadProject("finance", config)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Quality.Diagnostics) != 0 {
		t.Fatalf("free-form prose produced semantic diagnostics: %#v", candidate.Quality.Diagnostics)
	}
}

func TestGraphAndStructuredClaimDiagnosticsUseTypedEvidence(t *testing.T) {
	graphConfig := writeProject(t, "graph", map[string]string{"model.ossie.yaml": qualityDisconnectedModel}, projectFor("model.ossie.yaml"))
	graphCandidate, err := LoadProject("finance", graphConfig)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticCodes(t, graphCandidate.Quality.Diagnostics, DiagnosticDisconnectedDataset, DiagnosticUnreachableDimension)

	claimConfig := writeProject(t, "claims", map[string]string{"model.ossie.yaml": qualityClaimsModel}, projectFor("model.ossie.yaml"))
	claimCandidate, err := LoadProject("finance", claimConfig)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticCodes(t, claimCandidate.Quality.Diagnostics, DiagnosticMetricAggregationMismatch, DiagnosticMetricTypeMismatch)

	ambiguousConfig := writeProject(t, "ambiguous", map[string]string{"model.ossie.yaml": qualityAmbiguousGraphModel}, projectFor("model.ossie.yaml"))
	ambiguousCandidate, err := LoadProject("finance", ambiguousConfig)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagnosticCodes(t, ambiguousCandidate.Quality.Diagnostics, DiagnosticAmbiguousRelationshipPath)
}

func TestUnknownQualityPolicyCodeFailsDeterministically(t *testing.T) {
	config := writeProject(t, "unknown-policy", map[string]string{"model.ossie.yaml": qualityEquivalentModel}, `
semantic_sources:
  model:
    path: ./model.ossie.yaml
quality:
  severities:
    MODEL_QUALITY_Z_NOT_REGISTERED: error
    MODEL_QUALITY_A_NOT_REGISTERED: error
`)
	_, err := LoadProject("finance", config)
	var candidateErr *LoadError
	if !errors.As(err, &candidateErr) || candidateErr.Code != DiagnosticQualityEvaluation || !strings.Contains(err.Error(), `unknown model quality diagnostic code "MODEL_QUALITY_A_NOT_REGISTERED"`) {
		t.Fatalf("unknown policy error = %#v", err)
	}
}

func assertDiagnosticCodes(t *testing.T, diagnostics []Diagnostic, want ...DiagnosticCode) {
	t.Helper()
	actual := make([]DiagnosticCode, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		actual = append(actual, diagnostic.Code)
	}
	for _, code := range want {
		found := false
		for _, actualCode := range actual {
			found = found || actualCode == code
		}
		if !found {
			t.Fatalf("diagnostic %s missing from %v", code, actual)
		}
	}
}

const qualityEquivalentModel = `version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: sales
        source: analytics.sales
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: total_sales
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}
        custom_extensions: [{vendor_name: example.vendor, data: '{"opaque":true}'}]
      - name: sales_by_brand
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}
        custom_extensions: [{vendor_name: example.vendor, data: '{"opaque":true}'}]
`

const qualityAliasModel = `version: "0.2.0.dev0"
semantic_model:
  - name: MODEL_NAME
    datasets:
      - name: facts
        source: analytics.facts
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(facts.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"agent_discovery","aliases":["GMV"]}'
`

const qualityProseModel = `version: "0.2.0.dev0"
semantic_model:
  - name: retail
    ai_context: {warning: "deprecated relationship and grain contradiction"}
    datasets:
      - name: sales
        source: analytics.sales
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        description: "Must group by brand and should be deprecated"
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}
      - name: orders
        datatype: Integer
        description: "Same aggregation as revenue"
        expression: {dialects: [{dialect: ANSI_SQL, expression: "COUNT(sales.amount)"}]}
`

const qualityDisconnectedModel = `version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
      - name: customers
        source: analytics.customers
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customer_id}]}, dimension: {}}
    metrics:
      - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}}
`

const qualityClaimsModel = `version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: sales
        source: analytics.sales
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"quality_claims","expected_type":"Integer","expected_aggregation":"scalar"}'
`

const qualityAmbiguousGraphModel = `version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - {name: a, source: analytics.a, fields: [{name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}]}
      - {name: b, source: analytics.b, fields: [{name: id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: id}]}}]}
      - {name: c, source: analytics.c, fields: [{name: id, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: id}]}}]}
      - {name: d, source: analytics.d, fields: [{name: label, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: label}]}, dimension: {}}]}
    relationships:
      - {name: a_b, from: a, to: b, from_columns: [id], to_columns: [id]}
      - {name: b_d, from: b, to: d, from_columns: [id], to_columns: [id]}
      - {name: a_c, from: a, to: c, from_columns: [id], to_columns: [id]}
      - {name: c_d, from: c, to: d, from_columns: [id], to_columns: [id]}
    metrics:
      - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(a.amount)"}]}}
`
