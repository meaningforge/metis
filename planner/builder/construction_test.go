package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestRequiresComposed(t *testing.T) {
	direct := semanticplan.SourceAggregateNode{
		Base:   semanticplan.SemanticPlanNodeBase{ID: "orders"},
		Source: semanticplan.SemanticSourceState{Root: semanticplan.DatasetRef{Name: "orders"}},
	}
	derived := semanticplan.PostAggregateNode{
		Base:   semanticplan.SemanticPlanNodeBase{ID: "gross_margin"},
		Source: semanticplan.SemanticSourceState{Root: semanticplan.DatasetRef{Name: "orders"}},
	}

	if RequiresComposed([]semanticplan.SemanticPlanNode{direct}, nil) {
		t.Fatal("one source aggregate should remain direct")
	}
	if !RequiresComposed([]semanticplan.SemanticPlanNode{derived}, nil) {
		t.Fatal("a post-aggregate node must require composed construction")
	}
	if !RequiresComposed([]semanticplan.SemanticPlanNode{direct}, []semanticplan.PostEvaluationPredicate{{}}) {
		t.Fatal("a post-evaluation predicate must require composed construction")
	}
}

func TestSplitEvaluationPredicatesKeepsCumulativeTimeFilterPostEvaluation(t *testing.T) {
	plan := &evaluation.MetricEvaluationPlan{Nodes: []evaluation.MetricEvaluationNode{{
		ID:   "revenue_ytd",
		Kind: evaluation.MetricEvaluationCumulative,
		Spec: evaluation.MetricEvaluationSpec{Cumulative: &evaluation.CumulativeMetricEvaluationSpec{
			Spec: ossie.CumulativeMetricSpec{TimeDimension: "day_id"},
		}},
	}}}
	field := &ossie.Field{Name: "day_id"}
	pre, post, err := SplitEvaluationPredicates(plan, []semanticplan.GroupBy{{Name: "calendar.day_id", Dataset: "calendar", Field: field}}, []semanticplan.Predicate{{
		Filter:  query.Filter{Field: "calendar.day_id"},
		Dataset: "calendar",
		Field:   field,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) != 0 || len(post) != 1 || post[0].Name != "calendar.day_id" {
		t.Fatalf("predicate placement = pre:%#v post:%#v, want one post-evaluation predicate", pre, post)
	}
}

func TestRequireEvaluationSeamRejectsKindRewrite(t *testing.T) {
	before := CaptureEvaluationSeam([]semanticplan.SemanticPlanNode{
		semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	})
	err := RequireEvaluationSeam(before, []semanticplan.SemanticPlanNode{
		semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	})
	if err == nil {
		t.Fatal("rewriting a settled evaluation kind did not fail")
	}
}
