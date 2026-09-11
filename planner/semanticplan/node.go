package semanticplan

import (
	"fmt"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
)

// SemanticPlanNode is the closed semanticplan-owned family of typed SemanticPlan DAG
// nodes. Concrete node type is semantic authority.
type SemanticPlanNode interface {
	NodeBase() SemanticPlanNodeBase
	Kind() SemanticPlanNodeKind
	semanticPlanNode()
}

// SemanticPlanNodeKind is a stable derived projection of a concrete node type.
// Concrete node types, rather than this string, are the semantic authority.
type SemanticPlanNodeKind string

const (
	SemanticPlanNodeSourceAggregate     SemanticPlanNodeKind = "source_aggregate"
	SemanticPlanNodePostAggregate       SemanticPlanNodeKind = "post_aggregate"
	SemanticPlanNodeJoinAggregates      SemanticPlanNodeKind = "join_aggregates"
	SemanticPlanNodeCrossJoinAggregates SemanticPlanNodeKind = "cross_join_aggregates"
	SemanticPlanNodeCumulativeWindow    SemanticPlanNodeKind = "cumulative_window"
	SemanticPlanNodeTimeOffset          SemanticPlanNodeKind = "time_offset"
	SemanticPlanNodeOffsetToGrain       SemanticPlanNodeKind = "offset_to_grain"
	SemanticPlanNodeConversion          SemanticPlanNodeKind = "conversion"
	SemanticPlanNodeSemiAdditiveLast    SemanticPlanNodeKind = "semi_additive_last"
	SemanticPlanNodeSemiAdditiveFirst   SemanticPlanNodeKind = "semi_additive_first"
	SemanticPlanNodeSourceSelection     SemanticPlanNodeKind = "source_selection"
	SemanticPlanNodeAdditiveAttribution SemanticPlanNodeKind = "additive_attribution"
	SemanticPlanNodeRatioAttribution    SemanticPlanNodeKind = "ratio_attribution"
)

// SemanticPlanNodeBoundary is the node-vocabulary projection of the current
// semantic correctness boundary. It remains semantic evidence, not a physical
// SQL query-block boundary.
type SemanticPlanNodeBoundary string

const (
	SemanticPlanNodeBoundarySourceAggregate      SemanticPlanNodeBoundary = "source_aggregate"
	SemanticPlanNodeBoundaryPostAggregate        SemanticPlanNodeBoundary = "post_aggregate"
	SemanticPlanNodeBoundaryAggregateComposition SemanticPlanNodeBoundary = "aggregate_before_composition"
	SemanticPlanNodeBoundarySemanticIsolation    SemanticPlanNodeBoundary = "semantic_isolation"
	SemanticPlanNodeBoundarySourceSelection      SemanticPlanNodeBoundary = "source_selection"
)

// SemanticPlanNodeBase contains only state that is meaningful for every
// SemanticPlan DAG node. State shared by only some node families is composed
// separately below.
type SemanticPlanNodeBase struct {
	ID                        string
	Boundary                  SemanticPlanNodeBoundary
	PredicateBoundaryEvidence SemanticPredicateBoundaryEvidence
	Inputs                    []SemanticPlanNodeInput
	Dimensions                []string
	OutputGrain               []GroupBy
	Predicates                []SemanticPlanNodePredicate
	ExtensionEvidence         []extension.Evidence
}

type SemanticPlanNodeInput struct {
	NodeID string
	Grain  []GroupBy
}

type SemanticPlanNodePredicate struct {
	Scope       SemanticPredicateScope
	OwnerNodeID string
	Proof       SemanticPredicatePlacementProof
	Predicate   *Predicate
	Post        *PostEvaluationPredicate
}

// SemanticSourceState is composed only into source-aware node types.
type SemanticSourceState struct {
	SourceRoots                    []string
	RequiredDatasets               []string
	Root                           DatasetRef
	Joins                          []Join
	PopulationPreservationEvidence []PopulationPreservationEvidence
}

// SemanticMetricState is composed only into metric-bearing node types.
type SemanticMetricState struct {
	Metrics             []string
	ShareGroup          string
	SharedGrainEvidence *MetricSharedGrainEvidence
}

type SourceAggregateNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
	Rollup      RollupContract
}

type PostAggregateNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
}

type JoinAggregatesNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
}

type CrossJoinAggregatesNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
}

type CumulativeWindowNode struct {
	Base                      SemanticPlanNodeBase
	Source                    SemanticSourceState
	MetricState               SemanticMetricState
	Metric                    *ossie.Metric
	Expression                expression.ResolvedExpression
	Spec                      ossie.CumulativeMetricSpec
	CustomCalendarRolling     *CustomCalendarCumulativePlan
	CustomCalendarGrainToDate *CustomCalendarGrainToDatePlan
}

type TimeOffsetNode struct {
	Base           SemanticPlanNodeBase
	Source         SemanticSourceState
	MetricState    SemanticMetricState
	Metric         *ossie.Metric
	Expression     expression.ResolvedExpression
	Spec           ossie.TimeOffsetMetricSpec
	CustomCalendar *CustomCalendarOffsetPlan
}

type OffsetToGrainNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
	Spec        ossie.OffsetToGrainMetricSpec
	OffsetPlan  *OffsetToGrainPlan
}

type ConversionNode struct {
	Base           SemanticPlanNodeBase
	Source         SemanticSourceState
	MetricState    SemanticMetricState
	Metric         *ossie.Metric
	Expression     expression.ResolvedExpression
	Spec           ossie.ConversionMetricSpec
	Conversion     *ConversionPlan
	PhysicalInputs *ConversionPhysicalInputPlan
}

type SemiAdditiveNode struct {
	Base        SemanticPlanNodeBase
	Source      SemanticSourceState
	MetricState SemanticMetricState
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
	Spec        ossie.SemiAdditiveMetricSpec
}

type SourceSelectionNode struct {
	Base   SemanticPlanNodeBase
	Source SemanticSourceState
	Mode   SourceSelectionMode
}

func (node SourceAggregateNode) NodeBase() SemanticPlanNodeBase     { return node.Base }
func (node PostAggregateNode) NodeBase() SemanticPlanNodeBase       { return node.Base }
func (node JoinAggregatesNode) NodeBase() SemanticPlanNodeBase      { return node.Base }
func (node CrossJoinAggregatesNode) NodeBase() SemanticPlanNodeBase { return node.Base }
func (node CumulativeWindowNode) NodeBase() SemanticPlanNodeBase    { return node.Base }
func (node TimeOffsetNode) NodeBase() SemanticPlanNodeBase          { return node.Base }
func (node OffsetToGrainNode) NodeBase() SemanticPlanNodeBase       { return node.Base }
func (node ConversionNode) NodeBase() SemanticPlanNodeBase          { return node.Base }
func (node SemiAdditiveNode) NodeBase() SemanticPlanNodeBase        { return node.Base }
func (node SourceSelectionNode) NodeBase() SemanticPlanNodeBase     { return node.Base }

func (SourceAggregateNode) Kind() SemanticPlanNodeKind { return SemanticPlanNodeSourceAggregate }
func (PostAggregateNode) Kind() SemanticPlanNodeKind   { return SemanticPlanNodePostAggregate }
func (JoinAggregatesNode) Kind() SemanticPlanNodeKind  { return SemanticPlanNodeJoinAggregates }
func (CrossJoinAggregatesNode) Kind() SemanticPlanNodeKind {
	return SemanticPlanNodeCrossJoinAggregates
}
func (CumulativeWindowNode) Kind() SemanticPlanNodeKind { return SemanticPlanNodeCumulativeWindow }
func (TimeOffsetNode) Kind() SemanticPlanNodeKind       { return SemanticPlanNodeTimeOffset }
func (OffsetToGrainNode) Kind() SemanticPlanNodeKind    { return SemanticPlanNodeOffsetToGrain }
func (ConversionNode) Kind() SemanticPlanNodeKind       { return SemanticPlanNodeConversion }
func (SourceSelectionNode) Kind() SemanticPlanNodeKind  { return SemanticPlanNodeSourceSelection }

func (node SemiAdditiveNode) Kind() SemanticPlanNodeKind {
	switch node.Spec.Aggregation {
	case "last":
		return SemanticPlanNodeSemiAdditiveLast
	case "first":
		return SemanticPlanNodeSemiAdditiveFirst
	default:
		return ""
	}
}

func (SourceAggregateNode) semanticPlanNode()     {}
func (PostAggregateNode) semanticPlanNode()       {}
func (JoinAggregatesNode) semanticPlanNode()      {}
func (CrossJoinAggregatesNode) semanticPlanNode() {}
func (CumulativeWindowNode) semanticPlanNode()    {}
func (TimeOffsetNode) semanticPlanNode()          {}
func (OffsetToGrainNode) semanticPlanNode()       {}
func (ConversionNode) semanticPlanNode()          {}
func (SemiAdditiveNode) semanticPlanNode()        {}
func (SourceSelectionNode) semanticPlanNode()     {}

// ValidateNode rejects values outside the closed concrete node family. Pointer
// implementations are intentionally excluded so the family cannot acquire
// typed-nil interface values.
func ValidateNode(node SemanticPlanNode) error {
	switch node := node.(type) {
	case SourceAggregateNode,
		PostAggregateNode,
		JoinAggregatesNode,
		CrossJoinAggregatesNode,
		CumulativeWindowNode,
		TimeOffsetNode,
		OffsetToGrainNode,
		ConversionNode:
		return nil
	case AdditiveAttributionNode:
		return validateAdditiveAttributionNode(node)
	case RatioAttributionNode:
		return validateRatioAttributionNode(node)
	case SemiAdditiveNode:
		if node.Kind() == "" {
			return fmt.Errorf("unsupported semi-additive semantic plan node aggregation %q", node.Spec.Aggregation)
		}
		return nil
	case SourceSelectionNode:
		return validateSourceSelectionMode(node.Mode)
	case nil:
		return fmt.Errorf("semantic plan node is required")
	default:
		return fmt.Errorf("unsupported semantic plan node %T", node)
	}
}
