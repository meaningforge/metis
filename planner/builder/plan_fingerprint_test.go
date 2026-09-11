package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestFingerprintSemanticPlanIsDeterministicNonMutatingAndIgnoresTrace(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Model: semanticplan.ModelRef{Project: "analytics", Name: "sales"},
		Root:  semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
		Projections: []semanticplan.Projection{
			{Name: "revenue", Kind: semanticplan.ProjectionMetric, Datasets: []string{"orders", "customers"}},
		},

		OptimizationTrace: []semanticplan.OptimizationStep{{Rule: "join_deduplication", Changed: true}},
	}, []semanticNodeFixture{{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		SourceRoots:      []string{"orders"},
		RequiredDatasets: []string{"orders", "customers"},
		Metrics:          []string{"revenue"},
	}})
	before := semanticplan.ClonePlan(plan)
	before.Projections[0].Datasets = append([]string(nil), plan.Projections[0].Datasets...)
	// cloneSemanticPlan intentionally drops optimizer trace state because it is
	// used by optimizer transformations. Restore it here so this snapshot is a
	// faithful copy for the non-mutation assertion.
	before.OptimizationTrace = append([]semanticplan.OptimizationStep(nil), plan.OptimizationTrace...)

	first, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatalf("fingerprint first plan: %v", err)
	}
	second, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatalf("fingerprint second plan: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint changed across identical observations: first=%q second=%q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(first))
	}
	if !reflect.DeepEqual(plan, before) {
		t.Fatalf("fingerprinting mutated plan:\nbefore=%#v\nafter=%#v", before, plan)
	}

	withDifferentTrace := semanticplan.ClonePlan(plan)
	withDifferentTrace.OptimizationTrace = []semanticplan.OptimizationStep{{Rule: "source_scan_fusion", Changed: false}}
	traceFingerprint, err := semanticplan.Fingerprint(withDifferentTrace)
	if err != nil {
		t.Fatalf("fingerprint trace variant: %v", err)
	}
	if first != traceFingerprint {
		t.Fatalf("optimizer trace changed fingerprint: base=%q trace_variant=%q", first, traceFingerprint)
	}
}

func TestFingerprintSemanticPlanNormalizesMapAndSetOrdering(t *testing.T) {
	left := semanticPlanForTest(semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "revenue", Datasets: []string{"orders", "customers"}}},
		Groups: []semanticplan.GroupBy{{Name: "day", Dataset: "calendar", CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Levels: map[string]semanticplan.CustomCalendarLevel{
				"fiscal_week":    {BucketExpression: "week_start"},
				"fiscal_quarter": {BucketExpression: "quarter_start"},
			},
		}}},
	}, []semanticNodeFixture{{
		ID:               "revenue",
		RequiredDatasets: []string{"orders", "customers"},
		SourceRoots:      []string{"orders", "customers"},
	}})
	right := semanticPlanForTest(semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "revenue", Datasets: []string{"customers", "orders"}}},
		Groups: []semanticplan.GroupBy{{Name: "day", Dataset: "calendar", CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Levels: map[string]semanticplan.CustomCalendarLevel{
				"fiscal_quarter": {BucketExpression: "quarter_start"},
				"fiscal_week":    {BucketExpression: "week_start"},
			},
		}}},
	}, []semanticNodeFixture{{
		ID:               "revenue",
		RequiredDatasets: []string{"customers", "orders"},
		SourceRoots:      []string{"customers", "orders"},
	}})

	leftFingerprint, err := semanticplan.Fingerprint(left)
	if err != nil {
		t.Fatalf("fingerprint left plan: %v", err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatalf("fingerprint right plan: %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("equivalent map/set ordering produced different fingerprints: left=%q right=%q", leftFingerprint, rightFingerprint)
	}
}

func TestFingerprintSemanticPlanPreservesMeaningfulSequenceOrdering(t *testing.T) {
	left := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{
			{Name: "revenue", Kind: semanticplan.ProjectionMetric},
			{Name: "region", Kind: semanticplan.ProjectionDimension},
		},
	}
	right := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{
			{Name: "region", Kind: semanticplan.ProjectionDimension},
			{Name: "revenue", Kind: semanticplan.ProjectionMetric},
		},
	}

	leftFingerprint, err := semanticplan.Fingerprint(left)
	if err != nil {
		t.Fatalf("fingerprint left plan: %v", err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatalf("fingerprint right plan: %v", err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatalf("meaningful projection order was erased: fingerprint=%q", leftFingerprint)
	}
}

func TestFingerprintSemanticPlanChangesForStructuralChange(t *testing.T) {
	base := &semanticplan.SemanticPlan{
		Root:  semanticplan.DatasetRef{Name: "orders"},
		Joins: []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
	}
	changed := semanticplan.ClonePlan(base)
	changed.Joins = append(changed.Joins, semanticplan.Join{FromDataset: "orders", ToDataset: "products"})

	baseFingerprint, err := semanticplan.Fingerprint(base)
	if err != nil {
		t.Fatalf("fingerprint base plan: %v", err)
	}
	changedFingerprint, err := semanticplan.Fingerprint(changed)
	if err != nil {
		t.Fatalf("fingerprint changed plan: %v", err)
	}
	if baseFingerprint == changedFingerprint {
		t.Fatalf("structural change did not change fingerprint: %q", baseFingerprint)
	}
}

func TestFingerprintSemanticPlanRejectsNilPlan(t *testing.T) {
	if _, err := semanticplan.Fingerprint(nil); err == nil {
		t.Fatal("expected nil semantic plan to fail fingerprinting")
	}
}
