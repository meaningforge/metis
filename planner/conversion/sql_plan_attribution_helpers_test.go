package conversion

import (
	"strings"
	"testing"
	"time"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestPhysicalMetricAttributionInstantPreservesUTCPrecision(t *testing.T) {
	instant := time.Date(2026, 7, 1, 8, 30, 45, 123400000, time.FixedZone("UTC+8", 8*60*60))
	if got := physicalMetricAttributionInstant(instant); got != "2026-07-01 00:30:45.1234" {
		t.Fatalf("physical attribution instant = %q", got)
	}
}

func TestAdditiveAttributionPhysicalInputsFailClosedOnDerivedProducer(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
			semanticplan.AdditiveAttributionNode{Base: semanticplan.SemanticPlanNodeBase{Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}}}},
		},
	}
	_, _, err := additiveAttributionPhysicalInputs(plan)
	if err == nil || !strings.Contains(err.Error(), "source aggregate") {
		t.Fatalf("derived producer error = %v", err)
	}
}

func TestSourceWorkContainsDatasetDoesNotInventJoins(t *testing.T) {
	work := semanticplan.SourceScanWork{
		Root:             semanticplan.DatasetRef{Name: "orders"},
		RequiredDatasets: []string{"customers"},
		Joins:            []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
	}
	if !sourceWorkContainsDataset(work, "orders") || !sourceWorkContainsDataset(work, "customers") {
		t.Fatalf("known source work datasets were not recognized: %#v", work)
	}
	if sourceWorkContainsDataset(work, "calendar") {
		t.Fatal("attribution lowering invented an unavailable time-dimension join")
	}
}
