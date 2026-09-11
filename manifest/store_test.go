package manifest

import "testing"

func TestStoreSwap(t *testing.T) {
	a := &SemanticManifest{Version: "a", Projects: map[string]*ProjectIndex{"test": {Name: "test", Models: map[string]*ModelIndex{}}}}
	b := &SemanticManifest{Version: "b", Projects: map[string]*ProjectIndex{"test": {Name: "test", Models: map[string]*ModelIndex{}}}}
	store := NewStore(a)
	if got := store.Current().Version; got != "a" {
		t.Fatalf("got %q", got)
	}
	store.Swap(b)
	if got := store.Current().Version; got != "b" {
		t.Fatalf("got %q", got)
	}
}

func TestStoreLookupSwapsMatchingManifestAndGraph(t *testing.T) {
	a := &SemanticManifest{Version: "a", Digest: "sha256:a", Projects: map[string]*ProjectIndex{"test": {Name: "test", Models: map[string]*ModelIndex{}}}}
	b := &SemanticManifest{Version: "b", Digest: "sha256:b", Projects: map[string]*ProjectIndex{"test": {Name: "test", Models: map[string]*ModelIndex{}}}}
	store := NewStore(a)
	lookup, err := store.Lookup()
	if err != nil {
		t.Fatal(err)
	}
	if lookup == nil || lookup.ManifestDigest() != a.Digest {
		t.Fatalf("lookup digest = %q, want %q", lookup.ManifestDigest(), a.Digest)
	}

	store.Swap(b)
	lookup, err = store.Lookup()
	if err != nil {
		t.Fatal(err)
	}
	if lookup == nil || lookup.ManifestDigest() != b.Digest {
		t.Fatalf("lookup digest = %q, want %q", lookup.ManifestDigest(), b.Digest)
	}
}
