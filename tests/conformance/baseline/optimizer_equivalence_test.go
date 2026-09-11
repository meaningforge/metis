package baseline_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Optimized and
// unoptimized plans remain semantically equivalent, with explain/lineage
// invariance and deterministic fingerprints.
//
// Every scenario is checked on every supported target.
//
// What is deliberately not asserted is SQL equality. Optimization exists to
// change SQL; requiring it not to would require the optimizer to do nothing.
// What it must not change is meaning, which is what the assertions below are.

// planOptimizedAndUnoptimized runs the production path twice, differing only in
// whether the optimizer is present.
func planOptimizedAndUnoptimized(t *testing.T, scenario scenarios.Scenario, dialect string) (*semanticplan.SemanticPlan, *semanticplan.SemanticPlan) {
	t.Helper()

	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		t.Fatalf("unknown conformance fixture %q", scenario.Fixture)
	}
	document, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, document)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	semanticQuery := scenario.Query
	semanticQuery.Project = definition.Project
	semanticQuery.Model = definition.Model
	renderer := mustRenderer(t, dialect)

	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, renderer)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	optimized, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatalf("plan optimized: %v", err)
	}
	unoptimized, err := planner.New(nil).Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatalf("plan unoptimized: %v", err)
	}
	return optimized, unoptimized
}

// Explain and lineage are what an Agent is told the query means. If optimization
// can change them, then what the plan says and what it does are two things.
func TestOptimizationPreservesExplanationAndLineage(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				optimized, unoptimized := planOptimizedAndUnoptimized(t, scenario, target.Dialect)

				optimizedExplain, err := semanticplan.Explain(optimized)
				if err != nil {
					t.Errorf("%s: explain optimized: %v", scenario.Name, err)
					continue
				}
				unoptimizedExplain, err := semanticplan.Explain(unoptimized)
				if err != nil {
					t.Errorf("%s: explain unoptimized: %v", scenario.Name, err)
					continue
				}
				if !reflect.DeepEqual(optimizedExplain, unoptimizedExplain) {
					t.Errorf("%s: optimization changed the semantic explanation", scenario.Name)
				}
			}
		})
	}
}

// The result contract, which is the closest thing to an execution comparison
// that runs without an engine. Two plans that describe the same query and emit
// different columns cannot both be right, whatever their SQL looks like.
//
// This is not a claim that rows match. Real-engine execution is its own gate;
// what this rules out is a shape difference that would make row comparison
// meaningless.
func TestOptimizationPreservesTheOutputContract(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				optimized, unoptimized := planOptimizedAndUnoptimized(t, scenario, target.Dialect)

				optimizedSchema, err := conversion.BuildOutputSchema(optimized)
				if err != nil {
					t.Errorf("%s: output schema optimized: %v", scenario.Name, err)
					continue
				}
				unoptimizedSchema, err := conversion.BuildOutputSchema(unoptimized)
				if err != nil {
					t.Errorf("%s: output schema unoptimized: %v", scenario.Name, err)
					continue
				}
				if !reflect.DeepEqual(optimizedSchema, unoptimizedSchema) {
					t.Errorf("%s: optimization changed the output schema:\n  optimized=%#v\n  unoptimized=%#v",
						scenario.Name, optimizedSchema, unoptimizedSchema)
				}
			}
		})
	}
}

// Fingerprints are regression evidence, so they have to be a function of the
// plan and nothing else. A fingerprint that varied between observations of one
// plan would make every archived baseline unreadable.
func TestPlanFingerprintsAreDeterministic(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				optimized, unoptimized := planOptimizedAndUnoptimized(t, scenario, target.Dialect)

				for name, plan := range map[string]*semanticplan.SemanticPlan{"optimized": optimized, "unoptimized": unoptimized} {
					first, err := semanticplan.Fingerprint(plan)
					if err != nil {
						t.Errorf("%s (%s): %v", scenario.Name, name, err)
						continue
					}
					second, err := semanticplan.Fingerprint(plan)
					if err != nil {
						t.Errorf("%s (%s): %v", scenario.Name, name, err)
						continue
					}
					if first != second {
						t.Errorf("%s (%s): fingerprint is not a function of the plan: %s then %s",
							scenario.Name, name, first, second)
					}
				}
			}
		})
	}
}

// Verify the exact set of queries changed by optimization. Semantic equivalence
// alone would also pass if every optimization silently stopped firing. Comparing
// names catches one scenario disappearing while another takes its place.
func TestOptimizationStillChangesTheQueriesItShould(t *testing.T) {
	rewritten := []string{
		"derived_metric",
		"derived_null_negative_inputs",
		"metric_definition_filter_post_aggregation",
		"metric_filter_derived_metric",
		"metric_filter_hidden_metric",
		"metric_filter_multi_metric",
		"nested_derived_metric",
		"ratio_empty_population_null",
		"ratio_metric",
		"semantic_extension_derived_metric",
		"shared_grain_derived_with_source_metric",
	}

	for _, target := range evidence.CompilerTargets() {
		renderer := mustRenderer(t, target.Dialect)

		t.Run(target.Dialect, func(t *testing.T) {
			var observed []string
			for _, scenario := range scenarios.Core {
				optimized, unoptimized := planOptimizedAndUnoptimized(t, scenario, target.Dialect)

				optimizedSQL, err := renderPlan(renderer, optimized)
				if err != nil {
					t.Errorf("%s: render optimized: %v", scenario.Name, err)
					continue
				}
				unoptimizedSQL, err := renderPlan(renderer, unoptimized)
				if err != nil {
					t.Errorf("%s: render unoptimized: %v", scenario.Name, err)
					continue
				}
				if optimizedSQL != unoptimizedSQL {
					observed = append(observed, scenario.Name)
				}
			}
			sort.Strings(observed)

			if diff := difference(observed, rewritten); len(diff) != 0 {
				t.Errorf("optimization now rewrites these and they are not recorded:\n  %s", strings.Join(diff, "\n  "))
			}
			if diff := difference(rewritten, observed); len(diff) != 0 {
				t.Errorf("optimization no longer rewrites these:\n  %s\n\n"+
					"An optimizer that stopped firing passes every invariance check in this file. "+
					"If the change is deliberate, re-record it.", strings.Join(diff, "\n  "))
			}
		})
	}
}

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func renderPlan(renderer renderer.Renderer, plan *semanticplan.SemanticPlan) (string, error) {
	physical, err := conversion.BuildSQLPlan(plan, renderer)
	if err != nil {
		return "", err
	}
	rendered, err := renderer.Render(physical)
	return rendered.SQL, err
}
