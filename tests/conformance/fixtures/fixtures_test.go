package fixtures

import (
	"strings"
	"testing"
)

func TestCanonicalSemanticModelsAreUniqueAndDeterministic(t *testing.T) {
	models, err := CanonicalSemanticModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 2 {
		t.Fatalf("canonical semantic model count = %d, want a multi-model project", len(models))
	}
	seen := map[string]struct{}{}
	for i, model := range models {
		identity := model.Project + "/" + model.Model
		if _, exists := seen[identity]; exists {
			t.Fatalf("duplicate semantic model identity %q", identity)
		}
		seen[identity] = struct{}{}
		if i > 0 && models[i-1].Model > model.Model {
			t.Fatalf("models are not deterministic: %q before %q", models[i-1].Model, model.Model)
		}
	}
	commerce, ok := Lookup(CommerceAdversarial)
	if !ok {
		t.Fatal("missing adversarial commerce fixture")
	}
	if _, ok := seen[commerce.Project+"/"+commerce.Model]; !ok {
		t.Fatalf("shared commerce semantic model %q is missing", commerce.Model)
	}
}

func TestCanonicalSemanticModelsRejectConflictingSharedIdentity(t *testing.T) {
	_, err := canonicalSemanticModels([]Definition{
		{ID: "first", Project: "project", Model: "shared", Document: []byte("first")},
		{ID: "second", Project: "project", Model: "shared", Document: []byte("second")},
	})
	if err == nil || !strings.Contains(err.Error(), "different documents") {
		t.Fatalf("error=%v, want conflicting shared-model error", err)
	}
}
