package builder

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestSourceEvaluationSubplanIdentityIgnoresConsumerMetadata(t *testing.T) {
	base := semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
		SourceRoots:      []string{"orders", "payments"},
		RequiredDatasets: []string{"orders", "payments"},
		OutputGrain:      []semanticplan.GroupBy{{Dataset: "calendar", Name: "month"}},
	}
	otherConsumer := base
	otherConsumer.ID = "gross_revenue"
	otherConsumer.Inputs = []semanticNodeInputFixture{{NodeID: "some_other_metric"}}
	otherConsumer.ShareGroup = "source_999"

	left, err := sourceEvaluationSubplanIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	right, err := sourceEvaluationSubplanIdentity(otherConsumer)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("equivalent source subplans have different identities: %q != %q", left, right)
	}
}

func TestSourceEvaluationSubplanIdentityCanonicalizesSetLikeInputs(t *testing.T) {
	left := semanticNodeFixture{
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
		SourceRoots:      []string{"payments", "orders", "orders"},
		RequiredDatasets: []string{"payments", "orders", "payments"},
	}
	right := left
	right.SourceRoots = []string{"orders", "payments"}
	right.RequiredDatasets = []string{"orders", "payments"}

	leftID, err := sourceEvaluationSubplanIdentity(left)
	if err != nil {
		t.Fatal(err)
	}
	rightID, err := sourceEvaluationSubplanIdentity(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftID != rightID {
		t.Fatalf("set-equivalent source subplans have different identities: %q != %q", leftID, rightID)
	}
	if got := left.SourceRoots; len(got) != 3 || got[0] != "payments" {
		t.Fatalf("identity mutated caller source roots: %#v", got)
	}
}

func TestSourceEvaluationSubplanIdentityChangesWithSemanticWork(t *testing.T) {
	base := semanticNodeFixture{
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
		RequiredDatasets: []string{"orders"},
		OutputGrain:      []semanticplan.GroupBy{{Dataset: "calendar", Name: "month"}},
	}
	changed := base
	changed.OutputGrain = []semanticplan.GroupBy{{Dataset: "calendar", Name: "day"}}

	baseID, err := sourceEvaluationSubplanIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	changedID, err := sourceEvaluationSubplanIdentity(changed)
	if err != nil {
		t.Fatal(err)
	}
	if baseID == changedID {
		t.Fatalf("different semantic work produced the same identity %q", baseID)
	}
}

func TestSourceEvaluationSubplanIdentityRejectsNonSourceStage(t *testing.T) {
	_, err := sourceEvaluationSubplanIdentity(semanticNodeFixture{Kind: semanticplan.SemanticPlanNodePostAggregate})
	if err == nil {
		t.Fatal("expected non-source stage identity failure")
	}
}

// The property this identity gained by moving to the canonical projection.
//
// Identity decides whether two source-aggregate stages may share a scan. It used
// to be taken by reflecting over everything reachable from the fields it names,
// so a semanticplan.Join dragged in the whole ossie.Field it points at and a semanticplan.GroupBy dragged
// in the expression analysis behind its ResolvedExpression. Editing a metric
// description could therefore split a share group.
func TestSubplanIdentityIgnoresDocumentationAndDerivedEvidence(t *testing.T) {
	base := func() semanticNodeFixture {
		field := &ossie.Field{Name: "order_date", Datatype: ossie.DataTypeDate}
		return semanticNodeFixture{
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Root: semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			OutputGrain: []semanticplan.GroupBy{{
				Name: "order_date", Dataset: "orders", Field: field,
				Expression: expression.NewResolvedExpression("DUCKDB", "orders.order_date"),
			}},
		}
	}

	for _, tt := range []struct {
		name   string
		mutate func(stage *semanticNodeFixture)
	}{
		{
			name: "field documentation",
			mutate: func(stage *semanticNodeFixture) {
				stage.OutputGrain[0].Field.Description = "when the order was placed"
				stage.OutputGrain[0].Field.Label = "Order date"
			},
		},
		{
			name: "expression analysis",
			mutate: func(stage *semanticNodeFixture) {
				stage.OutputGrain[0].Expression = stage.OutputGrain[0].Expression.
					WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{})
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, err := sourceEvaluationSubplanIdentity(base())
			if err != nil {
				t.Fatal(err)
			}
			mutated := base()
			tt.mutate(&mutated)
			after, err := sourceEvaluationSubplanIdentity(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("non-structural state moved a subplan identity, which decides scan sharing\nbefore=%s\nafter= %s", before, after)
			}
		})
	}
}

// Two stages whose scan work is identical must share a scan, and this pins the
// consumer label that used to stop them.
//
// Two things decide scan sharing. semanticSourceStagesEquivalent forms the
// groups; sourceEvaluationSubplanIdentity then validates that everything inside
// a declared group is one subplan. They used to disagree, and not because they
// held different views of the semantics.
//
// Fusion compared []semanticNodePredicateFixture wholesale, and that struct carries
// OwnerNodeID. validatesemanticNodePredicateFixture requires a stage-owned
// predicate's owner to be that stage's own ID, so comparing it compared stage
// IDs -- which two distinct stages never share. Any two source stages that
// carried a predicate at all therefore refused to fuse, however identical their
// scan, joins, grain, and filters were. Identity was right all along: its
// contract says stage labels describe the consumer, not the reusable subplan.
//
// The assertion runs through fusePlanSourceScans rather than the
// equivalence helper, because the share group is the observable outcome and a
// helper returning true is not yet one. And it validates the graph afterwards
// rather than describing the fixture as valid: a fixture asserted to be
// realistic in a comment is only as good as the comment.
func TestStagesWithIdenticalScanWorkShareAScan(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		sharedScanStage("orders_count"),
		sharedScanStage("revenue"),
	}))
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("fixture is not a graph canonical validation accepts: %v", err)
	}
	fused, err := fusePlanSourceScans(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !fused {
		t.Fatal("fusion made no change; two stages reading the same rows through the same filter were not grouped")
	}
	// Fusion assigns share groups, and a group whose members disagree is an
	// Internal error at validation. Running it again is how that is checked.
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("the graph fusion produced does not validate: %v", err)
	}

	stages := semanticPlanNodeFixturesForTest(plan)
	left, right := stages[0], stages[1]
	if left.ShareGroup == "" {
		t.Fatal("fusion left the stages ungrouped, which means the predicate owner is being compared again")
	}
	if left.ShareGroup != right.ShareGroup {
		t.Fatalf("stages reading the same rows landed in different share groups: %q and %q", left.ShareGroup, right.ShareGroup)
	}

	// A group fusion forms but identity rejects fails closed at plan
	// validation, so the two definitions agreeing here is load-bearing.
	leftIdentity, err := sourceEvaluationSubplanIdentity(left)
	if err != nil {
		t.Fatal(err)
	}
	rightIdentity, err := sourceEvaluationSubplanIdentity(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftIdentity != rightIdentity {
		t.Fatal("fusion grouped these stages but identity would reject the group, " +
			"which fails closed at plan validation; the two definitions have drifted apart")
	}
}

// The owner label is the only thing removed. What a stage reads still separates
// it: a different filter, and a different scope for the same filter, both keep
// two stages in different groups.
func TestFusionStillSeparatesStagesThatReadDifferently(t *testing.T) {
	for _, tt := range []struct {
		name  string
		other semanticNodeFixture
	}{
		{name: "different filter", other: sharedScanStageWith("refunds", semanticplan.SemanticPredicatePreAggregation, "refunded")},
		{name: "different scope", other: sharedScanStageWith("refunds", semanticplan.SemanticPredicatePostAggregation, "paid")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan := semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
				sharedScanStage("revenue"),
				tt.other,
			}))
			if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
				t.Fatalf("fixture is not a graph canonical validation accepts: %v", err)
			}
			if _, err := fusePlanSourceScans(plan); err != nil {
				t.Fatal(err)
			}
			if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
				t.Fatalf("the graph fusion produced does not validate: %v", err)
			}
			for _, stage := range semanticPlanNodeFixturesForTest(plan) {
				if stage.ShareGroup != "" {
					t.Fatalf("fusion shared a scan between stages that do not read the same rows: %s is in %q",
						stage.ID, stage.ShareGroup)
				}
			}
		})
	}
}

func sharedScanStage(id string) semanticNodeFixture {
	return sharedScanStageWith(id, semanticplan.SemanticPredicatePreAggregation, "paid")
}

// sharedScanStageWith builds a source-aggregate stage that canonical validation
// accepts. The boundary evidence is derived from the boundary rather than
// written out, because validateSemanticPredicateBoundary requires them to match
// and a hand-written pair would only drift.
func sharedScanStageWith(id string, scope semanticplan.SemanticPredicateScope, status string) semanticNodeFixture {
	boundary := semanticplan.SemanticPlanNodeBoundarySourceAggregate
	return semanticNodeFixture{
		ID:                        id,
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  boundary,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(boundary),
		Root:                      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Predicates: []semanticNodePredicateFixture{{
			Scope:       scope,
			OwnerNodeID: id,
			Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
			Predicate:   &semanticplan.Predicate{Filter: query.Filter{Field: "status", Operator: query.FilterEQ, Value: status}},
		}},
	}
}

// Two stages carrying the same post-aggregation predicate read the same rows at
// the scan and differ only after it. Fusing them is therefore tempting and
// wrong: the shared CTE places every predicate at the scan, so the filter would
// move to the wrong side of the aggregate for both.
//
// #402 removed the owner label from the fusion comparison, which is what made
// this reachable -- before it, no two stages carrying any predicate fused at
// all, and the mistake was hidden behind a bug. Afterwards these two compared
// equal on everything fusion looked at.
//
// The fix is a refusal to describe rather than a refusal to plan. An ungrouped
// stage lowers correctly on its own, so the stages stay, unfused.
func TestFusionRefusesStagesWhoseFilterASharedScanWouldMisplace(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		sharedScanStageWith("revenue", semanticplan.SemanticPredicatePostAggregation, "paid"),
		sharedScanStageWith("orders_count", semanticplan.SemanticPredicatePostAggregation, "paid"),
	}))
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("fixture is not a graph canonical validation accepts: %v", err)
	}

	if _, err := fusePlanSourceScans(plan); err != nil {
		t.Fatalf("a stage a shared scan cannot serve must stay unfused, not fail planning: %v", err)
	}
	for _, stage := range semanticPlanNodeFixturesForTest(plan) {
		if stage.ShareGroup != "" {
			t.Fatalf("stage %s was put in share group %q; a shared scan would place its post-aggregation "+
				"filter before the aggregate", stage.ID, stage.ShareGroup)
		}
	}
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatalf("the graph fusion produced does not validate: %v", err)
	}
}

// The other half of the same refusal. Fusion declining to group such a stage is
// a choice it makes; a stage that arrives already carrying a share group has had
// that choice made for it by something else, and there is nothing safe to do
// with it. Canonical validation is where that has to fail closed, because it is
// the gate every graph passes before lowering builds the shared CTE.
func TestValidationRejectsAShareGroupAScanCannotServe(t *testing.T) {
	stage := sharedScanStageWith("revenue", semanticplan.SemanticPredicatePostAggregation, "paid")
	stage.ShareGroup = "source_001"
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{stage}))

	err := semanticplan.ValidateSemanticPlanDAG(&graph)
	if err == nil {
		t.Fatal("expected a share group whose stage a shared scan cannot serve to fail validation")
	}
	if !strings.Contains(err.Error(), "cannot be shared") {
		t.Fatalf("unexpected error: %v", err)
	}
}
