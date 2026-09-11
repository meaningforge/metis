package builder_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/builder"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestRequiredDataDependenciesSharedScenarios(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios.Core {
		t.Run(scenario.Name, func(t *testing.T) {
			fixture, ok := fixtures.Lookup(scenario.Fixture)
			if !ok {
				t.Fatal("fixture missing")
			}
			doc, err := ossie.NewLoader().Load(fixture.Document)
			if err != nil {
				t.Fatal(err)
			}
			m, err := manifest.BuildProjectManifest(scenario.Query.Project, doc)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := registry.Resolve("DUCKDB")
			if err != nil {
				t.Fatal(err)
			}
			q, err := resolver.New(manifest.NewStore(m)).ResolveForRenderer(context.Background(), scenario.Query, selected)
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := evaluation.BuildMetricEvaluationPlan(q)
			if err != nil {
				t.Fatal(err)
			}
			got, err := builder.RequiredDataDependencies(q, metrics)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Datasets) == 0 {
				t.Fatal("empty source workload")
			}
			again, err := builder.RequiredDataDependencies(q, metrics)
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatal("nondeterministic dependencies")
			}
			fields := map[manifest.SemanticReference]bool{}
			for _, field := range got.Fields {
				fields[field] = true
			}
			for _, dimension := range q.Dimensions {
				if !fields[manifest.SemanticReference{Dataset: dimension.Dataset, Field: dimension.Field.Name}] {
					t.Fatal("missing projected dimension")
				}
			}
			built, err := builder.Build(context.Background(), q, metrics, evaluation.RequiresMetricEvaluation(q), selected)
			if err != nil {
				t.Fatal(err)
			}
			datasets := map[string]bool{}
			for _, dataset := range got.Datasets {
				datasets[dataset] = true
			}
			for _, node := range built.Plan.Nodes {
				if source, ok := semanticplan.NodeSourceState(node); ok {
					if source.Root.Name != "" && !datasets[source.Root.Name] {
						t.Errorf("missing planned source %s", source.Root.Name)
					}
					for _, join := range source.Joins {
						for _, name := range join.Relationship.FromColumns {
							if !fields[manifest.SemanticReference{Dataset: join.Relationship.From, Field: name}] {
								t.Errorf("missing join dependency %s.%s", join.Relationship.From, name)
							}
						}
						for _, name := range join.Relationship.ToColumns {
							if !fields[manifest.SemanticReference{Dataset: join.Relationship.To, Field: name}] {
								t.Errorf("missing join dependency %s.%s", join.Relationship.To, name)
							}
						}
					}
				}
				for _, predicate := range node.NodeBase().Predicates {
					if p := predicate.Predicate; p != nil && p.Field != nil && !fields[manifest.SemanticReference{Dataset: p.Dataset, Field: p.Field.Name}] {
						t.Errorf("missing planned predicate dependency %s.%s", p.Dataset, p.Field.Name)
					}
				}
			}
		})
	}
}
