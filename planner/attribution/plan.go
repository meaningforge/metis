package attribution

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
)

// MetricAttributionExactness records the semantic guarantee Metis can make for
// an attribution plan. Attribution deliberately starts with a binary contract:
// either reconciliation is exact, or the analysis is unsupported.
type MetricAttributionExactness string

const (
	MetricAttributionExact       MetricAttributionExactness = "exact"
	MetricAttributionUnsupported MetricAttributionExactness = "unsupported"
)

// MetricAttributionComponentRole identifies how a canonical metric participates
// in the attribution algebra.
type MetricAttributionComponentRole string

const (
	MetricAttributionValue       MetricAttributionComponentRole = "value"
	MetricAttributionNumerator   MetricAttributionComponentRole = "numerator"
	MetricAttributionDenominator MetricAttributionComponentRole = "denominator"
)

// MetricAttributionReason is a stable planner-level fail-closed reason. Algebra
// details remain owned by manifest.MetricDecomposition rather than being copied
// into a second planner rule table.
type MetricAttributionReason string

const (
	MetricAttributionReasonUnsupportedDecomposition MetricAttributionReason = "unsupported_decomposition"
	MetricAttributionReasonUnsupportedMetricKind    MetricAttributionReason = "unsupported_metric_kind"
	MetricAttributionReasonTopologyMismatch         MetricAttributionReason = "decomposition_topology_mismatch"
)

// MetricAttributionComponent records only the canonical metric identity and its
// role in the attribution recipe. Aggregation function, rollup algebra, merge
// operator, and finalizer remain canonical manifest evidence derived from aggregation algebra and metric decomposition.
type MetricAttributionComponent struct {
	Metric string
	Role   MetricAttributionComponentRole
}

// MetricAttributionPlan is a source-independent semantic recipe for governed
// attribution. It never contains SQL, database connections, result rows, or an
// execution loop. Physical evidence queries remain a later compilation concern.
type MetricAttributionPlan struct {
	Metric         string
	Exactness      MetricAttributionExactness
	Strategy       semanticplan.MetricAttributionStrategy
	Components     []MetricAttributionComponent
	Reconciliation semanticplan.MetricAttributionReconciliation
	Reason         MetricAttributionReason
	Detail         string
}

// BuildMetricAttributionPlan projects the canonical manifest decomposition proof
// into query-scoped attribution IR.
//
// Metric decomposition is single-sourced in manifest.ModelIndex. This function
// does not inspect aggregation properties or reparse metric expressions to
// decide whether a metric is additive or a ratio. MetricEvaluationPlan remains
// authoritative for the query-scoped dependency topology.
func BuildMetricAttributionPlan(plan *evaluation.MetricEvaluationPlan, model *manifest.ModelIndex, metric string) (MetricAttributionPlan, error) {
	if err := evaluation.ValidateMetricEvaluationPlan(plan); err != nil {
		return MetricAttributionPlan{}, err
	}
	if model == nil {
		return MetricAttributionPlan{}, fmt.Errorf("metric attribution requires a manifest model")
	}
	metric = strings.TrimSpace(metric)
	if metric == "" {
		return MetricAttributionPlan{}, fmt.Errorf("metric attribution requires a metric")
	}

	nodes := make(map[string]evaluation.MetricEvaluationNode, len(plan.Nodes))
	for _, node := range plan.Nodes {
		nodes[node.ID] = node
	}
	node, ok := nodes[metric]
	if !ok {
		return MetricAttributionPlan{}, fmt.Errorf("metric %q is not present in the metric evaluation plan", metric)
	}

	var out MetricAttributionPlan
	switch decomposition := model.MetricDecomposition(metric).(type) {
	case manifest.AdditiveMetricDecomposition:
		out = buildAdditiveAttributionPlan(node)
	case manifest.RatioMetricDecomposition:
		out = buildRatioAttributionPlan(node, decomposition)
	case manifest.UnsupportedMetricDecomposition:
		out = unsupportedMetricAttribution(metric, MetricAttributionReasonUnsupportedDecomposition, decomposition.Reason)
	default:
		out = unsupportedMetricAttribution(metric, MetricAttributionReasonUnsupportedDecomposition, "unknown metric decomposition result")
	}
	if err := ValidateMetricAttributionPlan(out); err != nil {
		return MetricAttributionPlan{}, err
	}
	return out, nil
}

func buildAdditiveAttributionPlan(node evaluation.MetricEvaluationNode) MetricAttributionPlan {
	// Advanced evaluation kinds have additional population/time semantics and are
	// not admitted merely because their underlying manifest expression is additive.
	switch node.Kind {
	case evaluation.MetricEvaluationSource, evaluation.MetricEvaluationDerived:
		return MetricAttributionPlan{
			Metric:         node.ID,
			Exactness:      MetricAttributionExact,
			Strategy:       semanticplan.MetricAttributionAdditiveContribution,
			Components:     []MetricAttributionComponent{{Metric: node.ID, Role: MetricAttributionValue}},
			Reconciliation: semanticplan.MetricAttributionReconcileSegmentDelta,
		}
	default:
		return unsupportedMetricAttribution(node.ID, MetricAttributionReasonUnsupportedMetricKind, string(node.Kind))
	}
}

func buildRatioAttributionPlan(node evaluation.MetricEvaluationNode, decomposition manifest.RatioMetricDecomposition) MetricAttributionPlan {
	if node.Kind != evaluation.MetricEvaluationDerived {
		return unsupportedMetricAttribution(node.ID, MetricAttributionReasonUnsupportedMetricKind, string(node.Kind))
	}
	if !ratioDecompositionMatchesTopology(node, decomposition) {
		return unsupportedMetricAttribution(node.ID, MetricAttributionReasonTopologyMismatch, "manifest ratio operands do not match the query-scoped metric dependencies")
	}
	return MetricAttributionPlan{
		Metric:    node.ID,
		Exactness: MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionRatioMixRate,
		Components: []MetricAttributionComponent{
			{Metric: decomposition.Numerator, Role: MetricAttributionNumerator},
			{Metric: decomposition.Denominator, Role: MetricAttributionDenominator},
		},
		Reconciliation: semanticplan.MetricAttributionReconcileMixRate,
	}
}

func ratioDecompositionMatchesTopology(node evaluation.MetricEvaluationNode, decomposition manifest.RatioMetricDecomposition) bool {
	if strings.TrimSpace(decomposition.Numerator) == "" || strings.TrimSpace(decomposition.Denominator) == "" || decomposition.Numerator == decomposition.Denominator {
		return false
	}
	if len(node.Inputs) != 2 {
		return false
	}
	inputs := map[string]struct{}{
		node.Inputs[0].Metric: {},
		node.Inputs[1].Metric: {},
	}
	if _, ok := inputs[decomposition.Numerator]; !ok {
		return false
	}
	if _, ok := inputs[decomposition.Denominator]; !ok {
		return false
	}
	return true
}

func unsupportedMetricAttribution(metric string, reason MetricAttributionReason, detail string) MetricAttributionPlan {
	return MetricAttributionPlan{
		Metric:    metric,
		Exactness: MetricAttributionUnsupported,
		Reason:    reason,
		Detail:    detail,
	}
}

// ValidateMetricAttributionPlan keeps exact and unsupported plans as a closed
// contract so later explain, fingerprint, compile, or MCP layers cannot expose a
// partially populated plan as governed evidence.
func ValidateMetricAttributionPlan(plan MetricAttributionPlan) error {
	if strings.TrimSpace(plan.Metric) == "" {
		return fmt.Errorf("metric attribution plan has empty metric")
	}
	switch plan.Exactness {
	case MetricAttributionUnsupported:
		if plan.Reason == "" {
			return fmt.Errorf("unsupported metric attribution plan %q has no reason", plan.Metric)
		}
		if plan.Strategy != "" || len(plan.Components) != 0 || plan.Reconciliation != "" {
			return fmt.Errorf("unsupported metric attribution plan %q carries executable semantics", plan.Metric)
		}
		return nil
	case MetricAttributionExact:
		if plan.Reason != "" || plan.Detail != "" {
			return fmt.Errorf("exact metric attribution plan %q carries unsupported evidence", plan.Metric)
		}
	default:
		return fmt.Errorf("metric attribution plan %q has unsupported exactness %q", plan.Metric, plan.Exactness)
	}

	switch plan.Strategy {
	case semanticplan.MetricAttributionAdditiveContribution:
		if len(plan.Components) != 1 || plan.Components[0].Role != MetricAttributionValue || plan.Components[0].Metric != plan.Metric {
			return fmt.Errorf("additive attribution plan %q must carry its metric as one value component", plan.Metric)
		}
		if plan.Reconciliation != semanticplan.MetricAttributionReconcileSegmentDelta {
			return fmt.Errorf("additive attribution plan %q has invalid reconciliation %q", plan.Metric, plan.Reconciliation)
		}
	case semanticplan.MetricAttributionRatioMixRate:
		if len(plan.Components) != 2 || plan.Components[0].Role != MetricAttributionNumerator || plan.Components[1].Role != MetricAttributionDenominator {
			return fmt.Errorf("ratio attribution plan %q must carry numerator and denominator components", plan.Metric)
		}
		if strings.TrimSpace(plan.Components[0].Metric) == "" || strings.TrimSpace(plan.Components[1].Metric) == "" || plan.Components[0].Metric == plan.Components[1].Metric {
			return fmt.Errorf("ratio attribution plan %q has invalid component identities", plan.Metric)
		}
		if plan.Reconciliation != semanticplan.MetricAttributionReconcileMixRate {
			return fmt.Errorf("ratio attribution plan %q has invalid reconciliation %q", plan.Metric, plan.Reconciliation)
		}
	default:
		return fmt.Errorf("exact metric attribution plan %q has unsupported strategy %q", plan.Metric, plan.Strategy)
	}
	return nil
}
