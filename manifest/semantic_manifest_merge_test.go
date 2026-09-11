package manifest

import "testing"

func TestMergeManifestsBuildsOrderIndependentDigest(t *testing.T) {
	finance := &SemanticManifest{Version: "1", Digest: "sha256:finance", Projects: map[string]*ProjectIndex{"finance": {Name: "finance"}}}
	growth := &SemanticManifest{Version: "1", Digest: "sha256:growth", Projects: map[string]*ProjectIndex{"growth": {Name: "growth"}}}

	forward, err := MergeManifests(finance, growth)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := MergeManifests(growth, finance)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Digest == "" || forward.Digest != reverse.Digest {
		t.Fatalf("digests are not deterministic: forward=%q reverse=%q", forward.Digest, reverse.Digest)
	}
}
