package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestValidateConversionPhysicalLocalityAcceptsRootOwnedLinks(t *testing.T) {
	plan := semanticplan.ConversionPlan{
		BaseMetric:     "signup_events",
		BaseRoot:       "signups",
		ConversionRoot: "purchases",
		Entity: semanticplan.ConversionPropertyPlan{
			Base:       semanticplan.ConversionFieldRef{Dataset: "signups", Name: "user_id"},
			Conversion: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchaser_id"},
		},
		ConstantProperties: []semanticplan.ConversionPropertyPlan{{
			Base:       semanticplan.ConversionFieldRef{Dataset: "signups", Name: "session_id"},
			Conversion: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchase_session_id"},
		}},
	}
	if err := validateConversionPhysicalLocality(plan); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConversionPhysicalLocalityRejectsReachableRelatedField(t *testing.T) {
	plan := semanticplan.ConversionPlan{
		BaseMetric:     "signup_events",
		BaseRoot:       "signups",
		ConversionRoot: "purchases",
		Entity: semanticplan.ConversionPropertyPlan{
			Base:       semanticplan.ConversionFieldRef{Dataset: "accounts", Name: "user_id"},
			Conversion: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchaser_id"},
		},
	}
	if err := validateConversionPhysicalLocality(plan); err == nil {
		t.Fatal("expected related-dataset conversion link field to be rejected")
	}
}

func TestValidateConversionPhysicalLocalityRejectsRelatedConstantProperty(t *testing.T) {
	plan := semanticplan.ConversionPlan{
		BaseMetric:     "signup_events",
		BaseRoot:       "signups",
		ConversionRoot: "purchases",
		Entity: semanticplan.ConversionPropertyPlan{
			Base:       semanticplan.ConversionFieldRef{Dataset: "signups", Name: "user_id"},
			Conversion: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "purchaser_id"},
		},
		ConstantProperties: []semanticplan.ConversionPropertyPlan{{
			Base:       semanticplan.ConversionFieldRef{Dataset: "campaigns", Name: "campaign_id"},
			Conversion: semanticplan.ConversionFieldRef{Dataset: "purchases", Name: "campaign_id"},
		}},
	}
	if err := validateConversionPhysicalLocality(plan); err == nil {
		t.Fatal("expected related-dataset constant property to be rejected")
	}
}
