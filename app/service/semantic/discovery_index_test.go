package semantic

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
)

func TestIndexedSearchMatchesFullScanOrder(t *testing.T) {
	discovery := newService(t)
	index, err := discovery.discoveryIndex()
	if err != nil {
		t.Fatal(err)
	}
	project := discovery.manifest.Current().Projects["finance"]
	projectIndex := index.projects["finance"]
	for _, query := range []string{"revenue", "sales region", "orders customer", "Revenue Decimal", "margin"} {
		actual, searchErr := discovery.search(context.Background(), "finance", query)
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		want := referenceSearchOrder(projectIndex, project, normalize(query))
		got := make([]string, len(actual.Matches))
		for i, match := range actual.Matches {
			got[i] = fmt.Sprintf("%s|%s|%s|%d|%v", match.Kind, match.Model, match.Qualified, match.Score, match.MatchReasons)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("query %q indexed order mismatch\n got: %#v\nwant: %#v", query, got, want)
		}
	}
}

func TestDiscoveryIndexBuildIsDeterministic(t *testing.T) {
	semanticManifest := syntheticSemanticEstate(100)
	first, err := buildDiscoveryIndex(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildDiscoveryIndex(semanticManifest)
	if err != nil {
		t.Fatal(err)
	}
	firstProject, secondProject := first.projects["scale"], second.projects["scale"]
	if first.bytes != second.bytes || !reflect.DeepEqual(firstProject.models, secondProject.models) || !reflect.DeepEqual(firstProject.metrics, secondProject.metrics) || !reflect.DeepEqual(firstProject.assets, secondProject.assets) || !reflect.DeepEqual(firstProject.compatibilityEdges, secondProject.compatibilityEdges) {
		t.Fatal("repeated discovery index builds are not equivalent")
	}
	for _, terms := range [][]string{nil, {"needle"}, {"metric", "model"}} {
		if !reflect.DeepEqual(firstProject.modelSearch.candidates(terms), secondProject.modelSearch.candidates(terms)) || !reflect.DeepEqual(firstProject.metricSearch.candidates(terms), secondProject.metricSearch.candidates(terms)) || !reflect.DeepEqual(firstProject.assetSearch.candidates(terms), secondProject.assetSearch.candidates(terms)) {
			t.Fatalf("candidate order differs for %#v", terms)
		}
	}
}

func referenceSearchOrder(index *projectDiscoveryIndex, project *manifest.ProjectIndex, query string) []string {
	// This helper intentionally examines every indexed identity. It is the
	// correctness oracle for candidate narrowing, not another indexed lookup.
	type ranked struct {
		value string
		score int
		kind  AssetKind
		model string
		name  string
	}
	var matches []ranked
	for _, asset := range index.assets {
		model, _ := project.Model(asset.model)
		name, qualified, description := asset.name, asset.qualified, ""
		switch asset.kind {
		case AssetModel:
			description = model.Model.Description
		case AssetMetric:
			description = model.Metrics[asset.name].Description
		case AssetDimension:
			description = model.Fields[asset.qualified].Field.Description
		case AssetRelationship:
			relationship := model.Relationships[asset.name]
			description = relationship.From + " to " + relationship.To
		case AssetOntologyConcept:
			concept := project.Ontology[strings.ToLower(asset.name)]
			description = strings.TrimSpace(concept.Description + " " + concept.Type + " " + strings.Join(concept.Extends, " "))
		}
		score, reasons := rank(query, name, qualified, description)
		if score == 0 {
			continue
		}
		matches = append(matches, ranked{value: fmt.Sprintf("%s|%s|%s|%d|%v", asset.kind, asset.model, qualified, score, reasons), score: score, kind: asset.kind, model: asset.model, name: qualified})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].kind != matches[j].kind {
			return matches[i].kind < matches[j].kind
		}
		if matches[i].model != matches[j].model {
			return matches[i].model < matches[j].model
		}
		return matches[i].name < matches[j].name
	})
	out := make([]string, len(matches))
	for i := range matches {
		out[i] = matches[i].value
	}
	return out
}
