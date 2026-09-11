package compiler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

func TestAmbiguousRelationshipPathIsExplicit(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.AmbiguousPathsModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}

	query := query.SemanticQuery{
		Project: projectName,
		Model:   "ambiguous_paths",
		Metrics: []query.MetricRef{{Name: "revenue"}},
		Dimensions: []query.DimensionRef{
			{Name: "country"},
		},
	}

	_, err = resolver.New(manifest.NewStore(snapshot)).Resolve(context.Background(), query)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %#v, want typed serrors.Error", err)
	}
	if apiErr.Code != serrors.ErrAmbiguousRelationshipPath {
		t.Fatalf("error code = %s, want %s", apiErr.Code, serrors.ErrAmbiguousRelationshipPath)
	}
	if apiErr.Details == nil {
		t.Fatal("ambiguous relationship path error must include details")
	}
	if apiErr.Details["from"] != "orders" || apiErr.Details["to"] != "geography" {
		t.Fatalf("error details = %#v, want from=orders to=geography", apiErr.Details)
	}
}
