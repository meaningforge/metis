package compiler_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/builder"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/sqlplan"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestEverySharedScenarioScanRetainsRelationPolicy(t *testing.T) {
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
			built, err := builder.Build(context.Background(), q, metrics, evaluation.RequiresMetricEvaluation(q), selected)
			if err != nil {
				t.Fatal(err)
			}
			policies := map[string]*semanticplan.RelationPolicy{}
			for name := range q.Model.Datasets {
				policy, err := semanticplan.NewRelationPolicy("scope", name, []semanticplan.RelationPredicate{{Field: "tenant", Column: "tenant", Datatype: ossie.DataTypeString, Operator: query.FilterEQ, Values: []any{"private"}}})
				if err != nil {
					t.Fatal(err)
				}
				policies[name] = policy
			}
			constrained, err := semanticplan.WithRelationPolicies(built.Plan, "scope", policies)
			if err != nil {
				t.Fatal(err)
			}
			for _, optimize := range []bool{false, true} {
				plan := constrained
				if optimize && built.OptimizationMode != builder.OptimizationNone {
					if built.OptimizationMode == builder.OptimizationSemanticDAG {
						plan, err = optimizer.Default().OptimizeSemanticPlan(context.Background(), plan)
					} else {
						plan, err = optimizer.Default().Optimize(context.Background(), plan)
					}
					if err != nil {
						t.Fatalf("optimizer dropped policy: %v", err)
					}
				}
				if plan.PolicyScope != "scope" {
					t.Fatal("operation scope lost")
				}
				physical, err := conversion.BuildSQLPlan(plan, selected)
				if err != nil {
					t.Fatal(err)
				}
				scans := 0
				check := func(relation sqlplan.RelationRef) {
					if relation.Source != nil {
						t.Errorf("unfiltered physical scan escaped: %s", relation.Source.Name)
					}
					if relation.FilteredSource != nil {
						scans++
						if len(relation.FilteredSource.Predicates) != 1 {
							t.Error("predicate lost")
						}
					}
				}
				for _, block := range physical.Blocks {
					check(block.From)
					for _, join := range block.Joins {
						check(join.Relation)
					}
				}
				if scans == 0 {
					t.Fatal("no constrained physical scans")
				}
				if _, err := selected.Render(physical); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
