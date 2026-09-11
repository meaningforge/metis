package builder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Fusion decides which source stages share a scan; canonical validation checks
// that every stage in a share group agrees. Those are a producer and its checker,
// and they are only a real pair if they partition the stages the same way.
//
// Validation checks one direction: same group implies same identity. Nothing
// checked the other, and the other is where the two definitions used to differ.
// Fusion was the stricter of the two, so it kept stages apart that identity
// called the same subplan -- the gap was invisible because a checker that
// accepts more than the producer produces never fires.
//
// This asserts the biconditional over the whole corpus, on every compiler target.
// It is a grouping comparison rather than a hash comparison on purpose: what
// matters is that the two agree on which stages belong together, not that any
// particular identity string stays the same.
func TestFusionAndValidationPartitionSourceStagesIdentically(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			var withGroups int
			for _, scenario := range scenarios.Core {
				plan := planCorpusScenario(t, scenario, dialect)
				// Composed plans only, which is what this gate has always
				// measured -- it skipped on the wrapper's absence before, and
				// the wrapper was exactly the composed plans. Fusion groups
				// stages so they can share one CTE; a compact plan reads one
				// scan by construction, so there is no sharing to partition and
				// identity agreeing with an optimizer that never ran would be
				// agreement about nothing.
				if !semanticplan.RequiresComposedPlan(plan) {
					continue
				}

				byGroup := map[string][]string{}
				byIdentity := map[string][]string{}
				for _, stage := range semanticPlanNodeFixturesForTest(plan) {
					if stage.Kind != semanticplan.SemanticPlanNodeSourceAggregate {
						continue
					}
					identity, sharable, err := sharableSourceScanIdentity(stage)
					if err != nil {
						t.Fatalf("%s/%s: %v", scenario.Name, stage.ID, err)
					}
					if !sharable {
						// Groupable by nothing, and validation rejects it in a
						// group. Its own test covers that; it has no place in a
						// partition comparison.
						if stage.ShareGroup != "" {
							t.Errorf("%s: stage %s cannot be shared but carries share group %q",
								scenario.Name, stage.ID, stage.ShareGroup)
						}
						continue
					}
					if stage.ShareGroup != "" {
						byGroup[stage.ShareGroup] = append(byGroup[stage.ShareGroup], stage.ID)
					}
					byIdentity[identity] = append(byIdentity[identity], stage.ID)
				}

				// A share group is a non-singleton identity class, and every
				// non-singleton identity class is a share group. Singleton
				// classes get no label, so they are compared as the absence of
				// one.
				groups := partitionOf(byGroup)
				identities := partitionOf(byIdentity)
				if !equalPartitions(groups, identities) {
					t.Errorf("%s: fusion and identity disagree on which stages share a scan\n  fusion:   %s\n  identity: %s",
						scenario.Name, strings.Join(groups, " | "), strings.Join(identities, " | "))
				}
				if len(groups) != 0 {
					withGroups++
				}
			}

			// The comparison is only evidence if something in the corpus is
			// actually fused. If nothing is, both partitions are empty and this
			// test agrees with itself about nothing.
			if withGroups == 0 {
				t.Errorf("no scenario fused any source stage, so this comparison proves nothing")
			}
		})
	}
}

// partitionOf renders the non-singleton classes of a keyed grouping as sorted
// member lists, so two groupings can be compared without their keys -- share
// group names and identity hashes are different vocabularies for the same
// partition.
func partitionOf(byKey map[string][]string) []string {
	out := make([]string, 0, len(byKey))
	for _, members := range byKey {
		if len(members) < 2 {
			continue
		}
		sorted := append([]string(nil), members...)
		sort.Strings(sorted)
		out = append(out, strings.Join(sorted, ","))
	}
	sort.Strings(out)
	return out
}

func equalPartitions(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func planCorpusScenario(t *testing.T, scenario scenarios.Scenario, dialect string) *semanticplan.SemanticPlan {
	t.Helper()
	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		t.Fatalf("unknown conformance fixture %q", scenario.Fixture)
	}
	document, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, document)
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery := scenario.Query
	semanticQuery.Project = definition.Project
	semanticQuery.Model = definition.Model
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(
		context.Background(), semanticQuery, mustRenderer(t, dialect))
	if err != nil {
		t.Fatal(err)
	}
	evaluationPlan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	built, err := Build(context.Background(), resolved, evaluationPlan, evaluation.RequiresMetricEvaluation(resolved), mustRenderer(t, dialect))
	if err != nil {
		t.Fatal(fmt.Errorf("plan %s: %w", scenario.Name, err))
	}
	if built.OptimizationMode != OptimizationSemanticDAG {
		return built.Plan
	}
	plan, err := optimizer.Default().OptimizeSemanticPlan(context.Background(), built.Plan)
	if err != nil {
		t.Fatal(fmt.Errorf("optimize %s: %w", scenario.Name, err))
	}
	return plan
}
