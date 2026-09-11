package builder

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func TestValidateConversionQueryGrainsAllowsBaseOwnedGroup(t *testing.T) {
	nodes := conversionQueryGrainNodes()
	groups := []semanticplan.GroupBy{{Name: "campaign", Dataset: "signups"}}
	if err := validateConversionQueryGrains(nodes, groups); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConversionQueryGrainsRejectsConversionOwnedGroup(t *testing.T) {
	nodes := conversionQueryGrainNodes()
	groups := []semanticplan.GroupBy{{Name: "purchase_channel", Dataset: "purchases"}}
	err := validateConversionQueryGrains(nodes, groups)
	if err == nil {
		t.Fatal("expected conversion-owned query grain to be rejected")
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConversionQueryGrainsAllowsCustomCalendarOfBaseTime(t *testing.T) {
	nodes := conversionQueryGrainNodes()
	groups := []semanticplan.GroupBy{{
		Name: "signup_time",
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			BaseTimeField: &ossie.Field{Name: "signup_time"},
		},
	}}
	if err := validateConversionQueryGrains(nodes, groups); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConversionQueryGrainsRejectsCustomCalendarOfConversionTime(t *testing.T) {
	nodes := conversionQueryGrainNodes()
	groups := []semanticplan.GroupBy{{
		Name: "purchase_time",
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			BaseTimeField: &ossie.Field{Name: "purchase_time"},
		},
	}}
	if err := validateConversionQueryGrains(nodes, groups); err == nil {
		t.Fatal("expected conversion-time custom calendar grain to be rejected")
	}
}

func conversionQueryGrainNodes() []semanticplan.SemanticPlanNode {
	return []semanticplan.SemanticPlanNode{semanticplan.ConversionNode{
		Base: semanticplan.SemanticPlanNodeBase{ID: "signup_to_purchase_rate"},
		Conversion: &semanticplan.ConversionPlan{
			BaseRoot:       "signups",
			ConversionRoot: "purchases",
			BaseTime:       semanticplan.ConversionFieldRef{Dataset: "signups", Name: "signup_time"},
			ConversionTime: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchase_time"},
		},
	}}
}
