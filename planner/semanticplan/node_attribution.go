package semanticplan

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MetricAttributionTimeRange is exact half-open period evidence carried by an
// attribution node. Natural-language time interpretation remains outside the
// semantic-plan IR.
type MetricAttributionTimeRange struct {
	Start time.Time
	End   time.Time
}

// MetricAttributionStrategy is the deterministic algebra an attribution node
// represents. The attribution workflow selects it; the closed node and its
// read-only explanation carry the resulting semantic fact.
type MetricAttributionStrategy string

const (
	MetricAttributionAdditiveContribution MetricAttributionStrategy = "additive_contribution"
	MetricAttributionRatioMixRate         MetricAttributionStrategy = "ratio_mix_rate"
)

// MetricAttributionReconciliation is the invariant an attribution node's
// physical evidence must preserve.
type MetricAttributionReconciliation string

const (
	MetricAttributionReconcileSegmentDelta MetricAttributionReconciliation = "sum_segment_delta_equals_metric_delta"
	MetricAttributionReconcileMixRate      MetricAttributionReconciliation = "sum_mix_plus_rate_equals_metric_delta"
)

// RatioAttributionUndefinedRatioPolicy is the evidence contract for a
// denominator-zero condition during ratio attribution.
type RatioAttributionUndefinedRatioPolicy string

const RatioAttributionUndefinedNullWithDefinedFlag RatioAttributionUndefinedRatioPolicy = "null_with_defined_flag"

// RatioAttributionPopulationAlignment distinguishes segment absence from a
// present segment whose denominator is zero.
type RatioAttributionPopulationAlignment string

const RatioAttributionFullUnionEntryExit RatioAttributionPopulationAlignment = "full_union_entry_exit"

// AdditiveAttributionNode is the typed semantic operator for one exact
// additive change decomposition. Its concrete type is the semantic authority;
// strategy and reconciliation are derived by the attribution workflow.
type AdditiveAttributionNode struct {
	Base             SemanticPlanNodeBase
	MetricState      SemanticMetricState
	MetricRef        string
	TimeDimensionRef string
	TimeDimension    GroupBy
	DimensionRef     string
	Baseline         MetricAttributionTimeRange
	Current          MetricAttributionTimeRange
}

func (node AdditiveAttributionNode) NodeBase() SemanticPlanNodeBase { return node.Base }
func (AdditiveAttributionNode) Kind() SemanticPlanNodeKind {
	return SemanticPlanNodeAdditiveAttribution
}
func (AdditiveAttributionNode) semanticPlanNode() {}

// RatioAttributionNode is the typed semantic operator for one exact ratio
// change decomposition. Inputs remain ordered numerator then denominator.
type RatioAttributionNode struct {
	Base             SemanticPlanNodeBase
	MetricState      SemanticMetricState
	MetricRef        string
	NumeratorRef     string
	DenominatorRef   string
	TimeDimensionRef string
	TimeDimension    GroupBy
	DimensionRef     string
	Baseline         MetricAttributionTimeRange
	Current          MetricAttributionTimeRange
}

func (node RatioAttributionNode) NodeBase() SemanticPlanNodeBase { return node.Base }
func (RatioAttributionNode) Kind() SemanticPlanNodeKind {
	return SemanticPlanNodeRatioAttribution
}
func (RatioAttributionNode) semanticPlanNode() {}

func validateAdditiveAttributionNode(node AdditiveAttributionNode) error {
	if strings.TrimSpace(node.MetricRef) == "" || strings.TrimSpace(node.TimeDimensionRef) == "" || strings.TrimSpace(node.DimensionRef) == "" {
		return fmt.Errorf("additive attribution node requires metric, time-dimension, and decomposition-dimension refs")
	}
	if node.DimensionRef == node.TimeDimensionRef {
		return fmt.Errorf("additive attribution decomposition dimension must differ from its time dimension")
	}
	if node.TimeDimension.Name != node.TimeDimensionRef {
		return fmt.Errorf("additive attribution time dimension evidence must match ref %q", node.TimeDimensionRef)
	}
	if strings.TrimSpace(node.TimeDimension.Dataset) == "" || !node.TimeDimension.Expression.IsResolved() {
		return fmt.Errorf("additive attribution time dimension %q requires resolved dataset and expression evidence", node.TimeDimensionRef)
	}
	if err := ValidateMetricAttributionTimeRange("baseline", node.Baseline); err != nil {
		return err
	}
	if err := ValidateMetricAttributionTimeRange("current", node.Current); err != nil {
		return err
	}
	if len(node.Base.Inputs) != 1 || strings.TrimSpace(node.Base.Inputs[0].NodeID) == "" {
		return fmt.Errorf("additive attribution node requires exactly one semantic input")
	}
	if len(node.Base.Dimensions) != 1 || node.Base.Dimensions[0] != node.DimensionRef {
		return fmt.Errorf("additive attribution node dimensions must contain exactly its decomposition dimension")
	}
	if len(node.Base.OutputGrain) != 1 || node.Base.OutputGrain[0].Name != node.DimensionRef {
		return fmt.Errorf("additive attribution node output grain must be exactly its decomposition dimension")
	}
	if canonicalGrainKey(node.Base.Inputs[0].Grain) != canonicalGrainKey(node.Base.OutputGrain) {
		return fmt.Errorf("additive attribution node input and output grains must match")
	}
	if len(node.Base.Predicates) != 0 {
		return fmt.Errorf("additive attribution node cannot own shared filters; they belong to its semantic input")
	}
	if len(node.MetricState.Metrics) != 1 || node.MetricState.Metrics[0] != node.MetricRef || node.MetricState.ShareGroup != "" || node.MetricState.SharedGrainEvidence != nil {
		return fmt.Errorf("additive attribution node metric state must contain exactly its attributed metric")
	}
	return nil
}

func validateRatioAttributionNode(node RatioAttributionNode) error {
	if strings.TrimSpace(node.MetricRef) == "" || strings.TrimSpace(node.NumeratorRef) == "" || strings.TrimSpace(node.DenominatorRef) == "" || strings.TrimSpace(node.TimeDimensionRef) == "" || strings.TrimSpace(node.DimensionRef) == "" {
		return fmt.Errorf("ratio attribution node requires metric, operand, time-dimension, and decomposition-dimension refs")
	}
	if node.NumeratorRef == node.DenominatorRef || node.MetricRef == node.NumeratorRef || node.MetricRef == node.DenominatorRef {
		return fmt.Errorf("ratio attribution node requires distinct target, numerator, and denominator refs")
	}
	if node.DimensionRef == node.TimeDimensionRef {
		return fmt.Errorf("ratio attribution decomposition dimension must differ from its time dimension")
	}
	if node.TimeDimension.Name != node.TimeDimensionRef {
		return fmt.Errorf("ratio attribution time dimension evidence must match ref %q", node.TimeDimensionRef)
	}
	if strings.TrimSpace(node.TimeDimension.Dataset) == "" || !node.TimeDimension.Expression.IsResolved() {
		return fmt.Errorf("ratio attribution time dimension %q requires resolved dataset and expression evidence", node.TimeDimensionRef)
	}
	if err := ValidateMetricAttributionTimeRange("baseline", node.Baseline); err != nil {
		return err
	}
	if err := ValidateMetricAttributionTimeRange("current", node.Current); err != nil {
		return err
	}
	if len(node.Base.Inputs) != 2 || node.Base.Inputs[0].NodeID != node.NumeratorRef || node.Base.Inputs[1].NodeID != node.DenominatorRef {
		return fmt.Errorf("ratio attribution node inputs must be ordered numerator then denominator")
	}
	if len(node.Base.Dimensions) != 1 || node.Base.Dimensions[0] != node.DimensionRef {
		return fmt.Errorf("ratio attribution node dimensions must contain exactly its decomposition dimension")
	}
	if len(node.Base.OutputGrain) != 1 || node.Base.OutputGrain[0].Name != node.DimensionRef {
		return fmt.Errorf("ratio attribution node output grain must be exactly its decomposition dimension")
	}
	wantGrain := canonicalGrainKey(node.Base.OutputGrain)
	for _, input := range node.Base.Inputs {
		if canonicalGrainKey(input.Grain) != wantGrain {
			return fmt.Errorf("ratio attribution node input and output grains must match")
		}
	}
	if len(node.Base.Predicates) != 0 {
		return fmt.Errorf("ratio attribution node cannot own shared filters; they belong to its semantic inputs")
	}
	if len(node.MetricState.Metrics) != 1 || node.MetricState.Metrics[0] != node.MetricRef || node.MetricState.ShareGroup != "" || node.MetricState.SharedGrainEvidence != nil {
		return fmt.Errorf("ratio attribution node metric state must contain exactly its attributed metric")
	}
	return nil
}

// ValidateMetricAttributionTimeRange checks exact half-open period evidence.
func ValidateMetricAttributionTimeRange(name string, period MetricAttributionTimeRange) error {
	if period.Start.IsZero() || period.End.IsZero() {
		return fmt.Errorf("metric attribution %s period requires concrete start and end instants", name)
	}
	if !period.Start.Before(period.End) {
		return fmt.Errorf("metric attribution %s period must be a non-empty half-open range", name)
	}
	return nil
}

func canonicalGrainKey(groups []GroupBy) string {
	if len(groups) == 0 {
		return "scalar"
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		grain := ""
		if group.Grain != nil {
			grain = string(*group.Grain)
		}
		custom := ""
		if group.CustomCalendar != nil {
			custom = string(group.CustomCalendar.Grain)
		}
		parts = append(parts, strings.Join([]string{group.Dataset, group.Name, grain, custom}, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// SameGrain reports whether two group lists describe the same logical grain.
// Attribution construction consumes this semantic-plan-owned comparison rather
// than reconstructing grain identity from node fields.
func SameGrain(left, right []GroupBy) bool {
	return canonicalGrainKey(left) == canonicalGrainKey(right)
}

func validateSourceSelectionMode(mode SourceSelectionMode) error {
	switch mode {
	case SourceSelectionGroupedValues, SourceSelectionDistinctValues:
		return nil
	case "":
		return fmt.Errorf("source-selection evaluation has no mode")
	default:
		return fmt.Errorf("unsupported source-selection mode %q", mode)
	}
}
