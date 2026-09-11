package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

func ontologyService(t *testing.T) (*DiscoveryService, context.Context) {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	doc.SemanticModel[0].Datasets[0].Fields[0].Dimension = &ossie.Dimension{}
	doc.Ontology = append(doc.Ontology, map[string]any{"concept": "Bonus", "type": "ValueType", "extends": []any{"Revenue"}})
	doc.OntologyMappings = append(doc.OntologyMappings, map[string]any{"name": "bonus", "concept_mappings": []map[string]any{
		{"concept": "Bonus", "object_mappings": []map[string]any{{"expression": "customer.region"}}},
	}})
	// A second Decimal dimension produces genuine ambiguity for Revenue.
	for j := range doc.SemanticModel[0].Datasets[1].Fields {
		f := &doc.SemanticModel[0].Datasets[1].Fields[j]
		if f.Name == "region" {
			f.Datatype = ossie.DataTypeDecimal
		}
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Projects["finance"].OntologyResolution.Ready() {
		t.Fatal(snapshot.Projects["finance"].OntologyResolution.Diagnostics())
	}
	return NewDiscoveryService(manifest.NewStore(snapshot)), auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "tenant", SubjectID: "subject", Scopes: []string{auth.ScopeAll}})
}

func TestOntologyPinnedGenerationAndProjectIsolation(t *testing.T) {
	s, ctx := ontologyService(t)
	initial := s.manifest.Current()
	req := OntologyResolutionRequest{ProjectID: "finance", ConceptRef: "ontology_concept:Revenue", Limit: 1}
	page, err := s.ResolveOntologyConcept(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	other, err := manifest.BuildProjectManifest("other", &ossie.Document{Version: initial.Version, Name: "Other ontology", Ontology: []map[string]any{{"concept": "Revenue", "type": "ValueType", "extends": []string{"Integer"}}}})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := manifest.MergeManifests(initial, other)
	if err != nil {
		t.Fatal(err)
	}
	s.manifest.Swap(merged)
	req.Cursor = page.NextCursor
	if _, err = s.ResolveOntologyConcept(ctx, req); err != nil {
		t.Fatalf("other project changed finance cursor: %v", err)
	}
	req.ProjectID = "other"
	if _, err = s.ResolveOntologyConcept(ctx, req); err == nil {
		t.Fatal("cross-project cursor accepted")
	}
	req.ProjectID = "finance"
	req.Cursor = ""
	// A store swap during visibility must not mix candidate generations.
	s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
		s.manifest.Swap(other)
		return AssetVisibilityDecision{AssetVisibilityVisible, AssetVisibilityReasonPolicyVisible}
	}))
	got, err := s.ResolveOntologyConcept(ctx, req)
	if err != nil || got.State != "ambiguous" {
		t.Fatalf("mixed generation: %+v %v", got, err)
	}
	s.manifest.Swap(initial)
	s.WithAssetVisibilityPolicy(AllVisibleAssetPolicy{}).WithGenerationIdentity("finance", 2)
	req.Cursor = page.NextCursor
	_, err = s.ResolveOntologyConcept(ctx, req)
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrSemanticPaginationRestart {
		t.Fatalf("generation error=%v", err)
	}
}

func TestOntologyResolutionVisibilityAndPagination(t *testing.T) {
	s, ctx := ontologyService(t)
	req := OntologyResolutionRequest{ProjectID: "finance", ConceptRef: "ontology_concept:Revenue", Limit: 1}
	first, err := s.ResolveOntologyConcept(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "ambiguous" || len(first.Candidates) != 1 || !first.Truncated || first.Candidates[0].Evidence[0].MatchKind != "direct" {
		t.Fatalf("%+v", first)
	}
	req.Cursor = first.NextCursor
	next, err := s.ResolveOntologyConcept(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "ambiguous" || next.Truncated || next.Candidates[0].Evidence[0].MatchKind != "descendant" {
		t.Fatalf("%+v", next)
	}
	req.Cursor = ""
	s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(_ context.Context, r AssetVisibilityRequest) AssetVisibilityDecision {
		if r.AssetRef == "dimension:sales.customer.region" {
			return AssetVisibilityDecision{AssetVisibilityHidden, AssetVisibilityReasonPolicyHidden}
		}
		return AssetVisibilityDecision{AssetVisibilityVisible, AssetVisibilityReasonPolicyVisible}
	}))
	result, err := s.ResolveOntologyConcept(ctx, req)
	if err != nil || result.State != "unique" {
		t.Fatalf("%+v %v", result, err)
	}
	s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
		return AssetVisibilityDecision{AssetVisibilityHidden, AssetVisibilityReasonPolicyUnavailable}
	}))
	if result, err = s.ResolveOntologyConcept(ctx, req); err == nil || result != nil {
		t.Fatal("policy outage returned partial evidence")
	}
}

func TestOntologyConceptSearchDoesNotProbeMappingText(t *testing.T) {
	s, ctx := ontologyService(t)
	for _, q := range []string{"orders", "customer.region"} {
		result, err := s.SearchOntologyConcepts(ctx, OntologyConceptSearchRequest{ProjectID: "finance", Query: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Concepts) != 0 || result.Truncated || result.NextCursor != "" {
			t.Fatalf("hidden mapping probe: %+v", result)
		}
	}
	result, err := s.SearchOntologyConcepts(ctx, OntologyConceptSearchRequest{ProjectID: "finance", Query: "Revenue", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Concepts) != 1 || result.Concepts[0].ConceptRef != "ontology_concept:Revenue" {
		t.Fatal(result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "orders") || strings.Contains(string(encoded), "mapped_expressions") {
		t.Fatal(string(encoded))
	}
}

func TestOntologyFailuresReturnNoPartialEvidence(t *testing.T) {
	for _, kind := range []string{"nil", "panic", "malformed", "hidden", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx := ontologyService(t)
			switch kind {
			case "nil":
				s.WithAssetVisibilityPolicy(nil)
			case "panic":
				s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision { panic("private adapter detail") }))
			case "malformed":
				s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
					return AssetVisibilityDecision{Effect: "invented", Reason: AssetVisibilityReasonPolicyVisible}
				}))
			case "hidden":
				s.WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
					return AssetVisibilityDecision{AssetVisibilityHidden, AssetVisibilityReasonPolicyHidden}
				}))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := s.ResolveOntologyConcept(ctx, OntologyResolutionRequest{ProjectID: "finance", ConceptRef: "ontology_concept:Revenue"})
			if kind == "hidden" {
				if err != nil || result.State != "no_match" || len(result.Candidates) != 0 {
					t.Fatalf("%+v %v", result, err)
				}
			} else {
				if err == nil || result != nil {
					t.Fatal("failure returned evidence")
				}
				if strings.Contains(err.Error(), "private") {
					t.Fatal("adapter cause leaked")
				}
			}
		})
	}
}
