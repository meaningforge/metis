package source

import (
	"strings"
	"testing"
)

func TestOntologyQualityBlocksInvalidMappingWithSourceLocation(t *testing.T) {
	ontology := `version: "0.2.0.dev0"
name: Business concepts
ontology:
  - concept: Region
    type: ValueType
    extends: [Integer]
ontology_mappings:
  - name: regions
    concept_mappings:
      - concept: Region
        object_mappings:
          - expression: orders.region
`
	config := writeProject(t, "bounded", map[string]string{"model.ossie.yaml": releaseSalesModel, "ontology.ossie.yaml": ontology}, `semantic_sources:
  all:
    path: ./*.ossie.yaml
`)
	candidate, err := LoadProject("finance", config)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Quality.Publishable {
		t.Fatal("mismatched mapping was publishable")
	}
	found := false
	for _, diag := range candidate.Quality.Diagnostics {
		if diag.Code == "ONTOLOGY_MAPPING_TYPE_MISMATCH" {
			found = true
			if diag.Severity != SeverityError || diag.Location == nil || diag.Location.Path != "ontology.ossie.yaml" {
				t.Fatalf("%+v", diag)
			}
		}
	}
	if !found {
		t.Fatal(candidate.Quality.Diagnostics)
	}
	supported := strings.Replace(ontology, "extends: [Integer]", "extends: [String]", 1)
	supported = strings.Replace(supported, "expression: orders.region", "expression: UPPER(orders.region)", 1)
	config = writeProject(t, "unsupported", map[string]string{"model.ossie.yaml": releaseSalesModel, "ontology.ossie.yaml": supported}, `semantic_sources:
  all:
    path: ./*.ossie.yaml
`)
	candidate, err = LoadProject("finance", config)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Quality.Publishable {
		t.Fatal(candidate.Quality.Diagnostics)
	}
	if len(candidate.Document.OntologyMappings) != 1 {
		t.Fatal("lost unsupported source")
	}
}
