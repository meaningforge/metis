package source

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDocumentCandidateMatchesLocalLoaderAndOwnsInputs(t *testing.T) {
	config := writeProject(t, "project", map[string]string{"model.ossie.yaml": releaseSalesModel}, projectFor("model.ossie.yaml"))
	local, err := LoadProject("finance", config)
	if err != nil {
		t.Fatal(err)
	}
	documents := []SourceDocument{{Source: local.Bundle.Documents[0].Source, Path: "model.ossie.yaml", Content: []byte(releaseSalesModel)}}
	remote, err := LoadProjectDocuments("finance", local.Config, documents)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(local.Bundle, remote.Bundle) || local.Manifest.Digest != remote.Manifest.Digest || !reflect.DeepEqual(local.Quality, remote.Quality) {
		t.Fatal("local and document assembly diverged")
	}
	documents[0].Content[0] = '!'
	delete(local.Config.SemanticSources, documents[0].Source)
	if string(remote.Bundle.Documents[0].Content) != releaseSalesModel || len(remote.Config.SemanticSources) != 1 {
		t.Fatal("document candidate retained mutable input")
	}
	for _, invalid := range [][]SourceDocument{nil, {remote.Bundle.Documents[0], remote.Bundle.Documents[0]}, {{Source: "unknown", Path: "model.yaml", Content: []byte(releaseSalesModel)}}, {{Source: documents[0].Source, Path: "model.yaml", Content: []byte(releaseSalesModel), Digest: "wrong"}}} {
		if _, err := LoadProjectDocuments("finance", remote.Config, invalid); err == nil {
			t.Fatal("invalid document identity accepted")
		}
	}
}

func TestLocalCandidateRetainsDiagnosticPaths(t *testing.T) {
	config := writeProject(t, "project", map[string]string{"model.ossie.yaml": "[invalid"}, projectFor("model.ossie.yaml"))
	_, err := LoadProject("finance", config)
	failure, ok := err.(*LoadError)
	if !ok || failure.Code != DiagnosticDocumentInvalid || failure.Location != filepath.Join(filepath.Dir(config), "model.ossie.yaml") {
		t.Fatalf("local diagnostic path changed: %v", err)
	}
}
