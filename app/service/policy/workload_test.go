package policy

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestCollectWorkloadSharedScenarios(t *testing.T) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios.Core {
		t.Run(scenario.Name, func(t *testing.T) {
			fixture, ok := fixtures.Lookup(scenario.Fixture)
			if !ok {
				t.Fatal("missing fixture")
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
			work := ResolvedWork{Query: q, Evaluation: metrics}
			principal := workload().Principal
			req, err := CollectWorkload(principal, q.Project, []ResolvedWork{work})
			if err != nil {
				t.Fatal(err)
			}
			union, err := CollectWorkload(principal, q.Project, []ResolvedWork{work, work})
			if err != nil || !reflect.DeepEqual(req, union) {
				t.Fatal("duplicate subquery changed workload")
			}
			decision := Decision{Effect: Constrained}
			for _, source := range req.Sources {
				decision.Sources = append(decision.Sources, SourceConstraint{Dataset: source.Dataset})
			}
			snapshot, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return decision, nil }), req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Bind(snapshot, req, map[string]*resolver.SemanticQuerySpec{q.Model.Model.Name: q}); err != nil {
				t.Fatal(err)
			}
			for index, source := range req.Sources {
				for _, field := range source.RequiredFields {
					deny := cloneDecision(decision)
					deny.Sources[index].DeniedFields = []FieldRef{field}
					if validateDecision(req, deny) != ErrDenied {
						t.Fatal("required dependency was not denied")
					}
				}
			}
			otherModel := *q.Model
			other := *q
			other.Model = &otherModel
			if _, err := CollectWorkload(principal, q.Project, []ResolvedWork{work, {Query: &other, Evaluation: metrics}}); err != ErrInvalid {
				t.Fatal("mixed model generations accepted")
			}
		})
	}
}
