package builder

import (
	"reflect"
	"testing"
	"time"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestValidateSemanticPlanNodeCoversEveryNodeKind(t *testing.T) {
	metric := &ossie.Metric{Name: "revenue"}
	grain := []semanticplan.GroupBy{{Name: "country"}}
	attribution := semanticplan.AdditiveAttributionNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:          "attribution",
			Inputs:      []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue", Grain: grain}},
			Dimensions:  []string{"country"},
			OutputGrain: grain,
		},
		MetricState:      semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
		MetricRef:        "revenue",
		TimeDimensionRef: "order_time",
		TimeDimension:    additiveAttributionTimeDimensionForTest(),
		DimensionRef:     "country",
		Baseline: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		Current: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	_, ratioAttribution := buildRatioAttributionNodeForTest(t)
	cases := []struct {
		name     string
		node     semanticplan.SemanticPlanNode
		wantKind semanticplan.SemanticPlanNodeKind
	}{
		{"source", semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "source"}, Metric: metric}, semanticplan.SemanticPlanNodeSourceAggregate},
		{"post", semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "post"}, Metric: metric}, semanticplan.SemanticPlanNodePostAggregate},
		{"join", semanticplan.JoinAggregatesNode{Base: semanticplan.SemanticPlanNodeBase{ID: "join"}, Metric: metric}, semanticplan.SemanticPlanNodeJoinAggregates},
		{"cross", semanticplan.CrossJoinAggregatesNode{Base: semanticplan.SemanticPlanNodeBase{ID: "cross"}, Metric: metric}, semanticplan.SemanticPlanNodeCrossJoinAggregates},
		{"cumulative", semanticplan.CumulativeWindowNode{Base: semanticplan.SemanticPlanNodeBase{ID: "cumulative"}, Metric: metric}, semanticplan.SemanticPlanNodeCumulativeWindow},
		{"time_offset", semanticplan.TimeOffsetNode{Base: semanticplan.SemanticPlanNodeBase{ID: "time_offset"}, Metric: metric}, semanticplan.SemanticPlanNodeTimeOffset},
		{"offset_to_grain", semanticplan.OffsetToGrainNode{Base: semanticplan.SemanticPlanNodeBase{ID: "offset_to_grain"}, Metric: metric}, semanticplan.SemanticPlanNodeOffsetToGrain},
		{"conversion", semanticplan.ConversionNode{Base: semanticplan.SemanticPlanNodeBase{ID: "conversion"}, Metric: metric}, semanticplan.SemanticPlanNodeConversion},
		{"semi_last", semanticplan.SemiAdditiveNode{Base: semanticplan.SemanticPlanNodeBase{ID: "semi_last"}, Metric: metric, Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "last"}}, semanticplan.SemanticPlanNodeSemiAdditiveLast},
		{"semi_first", semanticplan.SemiAdditiveNode{Base: semanticplan.SemanticPlanNodeBase{ID: "semi_first"}, Metric: metric, Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "first"}}, semanticplan.SemanticPlanNodeSemiAdditiveFirst},
		{"source_selection", semanticplan.SourceSelectionNode{Base: semanticplan.SemanticPlanNodeBase{ID: "source_selection"}, Mode: semanticplan.SourceSelectionGroupedValues}, semanticplan.SemanticPlanNodeSourceSelection},
		{"attribution", attribution, semanticplan.SemanticPlanNodeAdditiveAttribution},
		{"ratio_attribution", ratioAttribution, semanticplan.SemanticPlanNodeRatioAttribution},
	}

	seen := map[semanticplan.SemanticPlanNodeKind]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.node.Kind() != tc.wantKind {
				t.Fatalf("node kind = %q, want %q", tc.node.Kind(), tc.wantKind)
			}
			if tc.node.NodeBase().ID != tc.name {
				t.Fatalf("node id = %q, want %q", tc.node.NodeBase().ID, tc.name)
			}
			if err := semanticplan.ValidateNode(tc.node); err != nil {
				t.Fatalf("validate node: %v", err)
			}
			seen[tc.node.Kind()] = true
		})
	}

	want := []semanticplan.SemanticPlanNodeKind{
		semanticplan.SemanticPlanNodeSourceAggregate,
		semanticplan.SemanticPlanNodePostAggregate,
		semanticplan.SemanticPlanNodeJoinAggregates,
		semanticplan.SemanticPlanNodeCrossJoinAggregates,
		semanticplan.SemanticPlanNodeCumulativeWindow,
		semanticplan.SemanticPlanNodeTimeOffset,
		semanticplan.SemanticPlanNodeOffsetToGrain,
		semanticplan.SemanticPlanNodeConversion,
		semanticplan.SemanticPlanNodeSemiAdditiveLast,
		semanticplan.SemanticPlanNodeSemiAdditiveFirst,
		semanticplan.SemanticPlanNodeSourceSelection,
		semanticplan.SemanticPlanNodeAdditiveAttribution,
		semanticplan.SemanticPlanNodeRatioAttribution,
	}
	for _, kind := range want {
		if !seen[kind] {
			t.Fatalf("node kind %q is not covered", kind)
		}
	}
}

func TestSemanticPlanNodeCloneHelpersOwnConversionSlices(t *testing.T) {
	constantProperties := []ossie.ConversionPropertyPair{{BaseProperty: "campaign", ConversionProperty: "campaign"}}
	baseEventKey := []semanticplan.ConversionFieldRef{{Dataset: "visits", Name: "visit_id"}}
	physicalBaseEventKey := []semanticplan.ConversionPhysicalFieldRef{{Dataset: "visits", Name: "visit_id", Expression: "visit_id"}}

	spec := semanticplan.CloneConversionMetricSpec(ossie.ConversionMetricSpec{ConstantProperties: constantProperties})
	plan := semanticplan.CloneConversionPlan(&semanticplan.ConversionPlan{BaseEventKey: baseEventKey})
	physical := semanticplan.CloneConversionPhysicalInputPlan(&semanticplan.ConversionPhysicalInputPlan{BaseEventKey: physicalBaseEventKey})

	constantProperties[0].BaseProperty = "mutated"
	baseEventKey[0].Name = "mutated"
	physicalBaseEventKey[0].Name = "mutated"

	if got := spec.ConstantProperties[0].BaseProperty; got != "campaign" {
		t.Fatalf("canonical conversion spec aliased caller slice: %q", got)
	}
	if got := plan.BaseEventKey[0].Name; got != "visit_id" {
		t.Fatalf("canonical conversion plan aliased caller slice: %q", got)
	}
	if got := physical.BaseEventKey[0].Name; got != "visit_id" {
		t.Fatalf("canonical physical conversion input aliased caller slice: %q", got)
	}
}

func TestShadowSemanticPlanNodePreservesComposedState(t *testing.T) {
	stage := semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Dimensions:       []string{"country"},
		Metrics:          []string{"revenue"},
		SourceRoots:      []string{"orders"},
		RequiredDatasets: []string{"orders", "customers"},
		ShareGroup:       "shared_orders",
		Node: semanticplan.SourceAggregateNode{
			Base:   semanticplan.SemanticPlanNodeBase{ID: "revenue"},
			Metric: &ossie.Metric{Name: "revenue"},
		},
	}

	nodeValue, err := shadowSemanticPlanNode(stage)
	if err != nil {
		t.Fatalf("shadow node: %v", err)
	}
	node, ok := nodeValue.(semanticplan.SourceAggregateNode)
	if !ok {
		t.Fatalf("node type = %T, want semanticplan.SourceAggregateNode", nodeValue)
	}
	if !reflect.DeepEqual(node.Base.Dimensions, stage.Dimensions) {
		t.Fatalf("dimensions = %#v, want %#v", node.Base.Dimensions, stage.Dimensions)
	}
	if !reflect.DeepEqual(node.MetricState.Metrics, stage.Metrics) {
		t.Fatalf("metrics = %#v, want %#v", node.MetricState.Metrics, stage.Metrics)
	}
	if !reflect.DeepEqual(node.Source.SourceRoots, stage.SourceRoots) || !reflect.DeepEqual(node.Source.RequiredDatasets, stage.RequiredDatasets) {
		t.Fatalf("source state = %#v, want roots=%#v datasets=%#v", node.Source, stage.SourceRoots, stage.RequiredDatasets)
	}
	if node.MetricState.ShareGroup != stage.ShareGroup {
		t.Fatalf("share group = %q, want %q", node.MetricState.ShareGroup, stage.ShareGroup)
	}

	node.Base.Dimensions[0] = "changed"
	node.MetricState.Metrics[0] = "changed"
	node.Source.SourceRoots[0] = "changed"
	if stage.Dimensions[0] != "country" || stage.Metrics[0] != "revenue" || stage.SourceRoots[0] != "orders" {
		t.Fatal("mutating shadow node changed source stage")
	}
}

func TestValidateSemanticPlanNodeRejectsPointersAndTypedNil(t *testing.T) {
	pointer := &semanticplan.SourceAggregateNode{}
	if err := semanticplan.ValidateNode(pointer); err == nil {
		t.Fatal("expected pointer node to be rejected")
	}

	var nilPointer *semanticplan.SourceAggregateNode
	var node semanticplan.SemanticPlanNode = nilPointer
	if err := semanticplan.ValidateNode(node); err == nil {
		t.Fatal("expected typed-nil node to be rejected")
	}
}
