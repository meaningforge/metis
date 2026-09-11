package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestPlanConversionBuildsFanoutSafeCandidateAssignment(t *testing.T) {
	model := conversionPlannerModel(t)
	plan, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes())
	if err != nil {
		t.Fatal(err)
	}
	match := plan.CandidateMatch
	if len(match.PartitionBy) != 1 || match.PartitionBy[0] != (semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchase_id"}) {
		t.Fatalf("partition = %#v", match.PartitionBy)
	}
	if len(match.Equality) != 2 {
		t.Fatalf("equality links = %#v", match.Equality)
	}
	if match.Equality[0] != plan.Entity || match.Equality[1] != plan.ConstantProperties[0] {
		t.Fatalf("equality links = %#v", match.Equality)
	}
	if match.BaseTime != plan.BaseTime || match.ConversionTime != plan.ConversionTime {
		t.Fatalf("time predicate = %#v -> %#v", match.BaseTime, match.ConversionTime)
	}
	if match.Window == nil || match.Window.Count != 7 || match.Window.Unit != "day" {
		t.Fatalf("window = %#v", match.Window)
	}
	if len(match.OrderBy) != 2 {
		t.Fatalf("order = %#v", match.OrderBy)
	}
	if match.OrderBy[0].Field != plan.BaseTime || match.OrderBy[0].Direction != "desc" {
		t.Fatalf("primary order = %#v", match.OrderBy[0])
	}
	if match.OrderBy[1].Field != plan.BaseEventKey[0] || match.OrderBy[1].Direction != "desc" {
		t.Fatalf("tie break order = %#v", match.OrderBy[1])
	}
	if match.KeepRank != 1 {
		t.Fatalf("keep rank = %d", match.KeepRank)
	}
	if !match.AggregateBaseIndependently {
		t.Fatal("base population must be aggregated independently from candidate matches")
	}
}

func TestPlanConversionCandidateAssignmentCopiesMutableInputs(t *testing.T) {
	model := conversionPlannerModel(t)
	plan, err := planConversion(model, "signup_to_purchase_rate", conversionPlannerSpec(), conversionPlannerMetricEvaluationNodes())
	if err != nil {
		t.Fatal(err)
	}
	plan.ConversionEventKey[0].Name = "mutated"
	plan.ConstantProperties[0].Base.Name = "mutated"
	plan.Window.Count = 99
	if plan.CandidateMatch.PartitionBy[0].Name != "purchase_id" {
		t.Fatalf("candidate partition mutated with event key: %#v", plan.CandidateMatch.PartitionBy)
	}
	if plan.CandidateMatch.Equality[1].Base.Name != "session_id" {
		t.Fatalf("candidate equality mutated with property list: %#v", plan.CandidateMatch.Equality)
	}
	if plan.CandidateMatch.Window == nil || plan.CandidateMatch.Window.Count != 7 {
		t.Fatalf("candidate window mutated with public plan window: %#v", plan.CandidateMatch.Window)
	}
}
