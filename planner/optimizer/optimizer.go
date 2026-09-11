package optimizer

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

const maxOptimizationPasses = 16

// Rule is one deterministic semantic-plan rewrite.
type Rule interface {
	Name() string
	Apply(*semanticplan.SemanticPlan) (bool, error)
}

// Optimizer applies ordered rules to a cloned SemanticPlan until a deterministic
// fixed point is reached. It has no semantic resolution or renderer authority.
type Optimizer struct {
	preRule Rule
	rules   []Rule
}

// SourceScanFusionRule assigns deterministic share groups to equivalent
// source-aggregate reads when the semantic plan requires DAG composition.
type SourceScanFusionRule struct{}

func (SourceScanFusionRule) Name() string { return "source_scan_fusion" }

func (SourceScanFusionRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	if !semanticplan.RequiresComposedPlan(plan) {
		return false, nil
	}
	return fuseSourceScans(plan)
}

func (rule SourceScanFusionRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// SemanticSetNormalizationRule canonicalizes fields whose contract is a set.
type SemanticSetNormalizationRule struct{}

func (SemanticSetNormalizationRule) Name() string { return "semantic_set_normalization" }

func (SemanticSetNormalizationRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	changed := false
	for i := range plan.Projections {
		var didChange bool
		plan.Projections[i].Datasets, didChange = normalizeStringSet(plan.Projections[i].Datasets)
		changed = changed || didChange
	}
	if !semanticplan.RequiresComposedPlan(plan) {
		return changed, nil
	}
	for i, node := range plan.Nodes {
		source, ok := semanticplan.NodeSourceState(node)
		if !ok {
			continue
		}
		sourceChanged := false
		var didChange bool
		source.RequiredDatasets, didChange = normalizeStringSet(source.RequiredDatasets)
		sourceChanged = sourceChanged || didChange
		source.SourceRoots, didChange = normalizeStringSet(source.SourceRoots)
		sourceChanged = sourceChanged || didChange
		changed = changed || sourceChanged
		if sourceChanged {
			updated, err := semanticplan.WithNodeSourceState(node, source)
			if err != nil {
				return false, err
			}
			plan.Nodes[i] = updated
		}
	}
	return changed, nil
}

func (rule SemanticSetNormalizationRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// MetricProjectionPruningRule removes DAG nodes that cannot contribute to the
// requested metric projection, sort, or output-predicate dependency closure.
type MetricProjectionPruningRule struct{}

func (MetricProjectionPruningRule) Name() string { return "metric_projection_pruning" }

func (MetricProjectionPruningRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	if !semanticplan.RequiresComposedPlan(plan) {
		return false, nil
	}
	requirements, err := CollectRequirements(plan)
	if err != nil {
		return false, err
	}
	if len(requirements.metricNodes) == 0 {
		return false, nil
	}
	out := make([]semanticplan.SemanticPlanNode, 0, len(requirements.metricNodes))
	for _, node := range plan.Nodes {
		if node == nil || !requirements.RequiresNode(node.NodeBase().ID) {
			continue
		}
		if state, ok := semanticplan.NodeMetricState(node); ok {
			state.ShareGroup = ""
			updated, err := semanticplan.WithNodeMetricState(node, state)
			if err != nil {
				return false, err
			}
			node = updated
		}
		out = append(out, node)
	}
	if len(out) == len(plan.Nodes) {
		return false, nil
	}
	plan.Nodes = out
	return true, nil
}

func (rule MetricProjectionPruningRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// PredicateDeduplicationRule deduplicates both query predicates and the
// predicates owned by semantic DAG nodes.
type PredicateDeduplicationRule struct{}

func (PredicateDeduplicationRule) Name() string { return "predicate_deduplication" }

func (PredicateDeduplicationRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	changed := false
	plan.Predicates, changed = deduplicatePredicates(plan.Predicates)
	if !semanticplan.RequiresComposedPlan(plan) {
		return changed, nil
	}
	for i, node := range plan.Nodes {
		base := node.NodeBase()
		out := make([]semanticplan.SemanticPlanNodePredicate, 0, len(base.Predicates))
		for _, predicate := range base.Predicates {
			duplicate := false
			for _, existing := range out {
				if reflect.DeepEqual(existing, predicate) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				out = append(out, predicate)
			}
		}
		if len(out) == len(base.Predicates) {
			continue
		}
		base.Predicates = out
		updated, err := semanticplan.WithNodeBase(node, base)
		if err != nil {
			return false, err
		}
		plan.Nodes[i], changed = updated, true
	}
	return changed, nil
}

func (rule PredicateDeduplicationRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// PredicatePushdownRule moves query predicates to eligible source nodes only
// when their typed predicate-boundary evidence proves the movement safe.
type PredicatePushdownRule struct{}

func (PredicatePushdownRule) Name() string { return "predicate_pushdown" }

func (PredicatePushdownRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	if !semanticplan.RequiresComposedPlan(plan) || len(plan.Predicates) == 0 {
		return false, nil
	}
	blocked := false
	for _, node := range plan.Nodes {
		base := node.NodeBase()
		if base.Boundary == semanticplan.SemanticPlanNodeBoundarySemanticIsolation && base.PredicateBoundaryEvidence.Movement == semanticplan.SemanticPredicateBoundaryBlocked {
			blocked = true
			break
		}
	}
	if blocked {
		for _, node := range plan.Nodes {
			if _, ok := node.(semanticplan.SourceAggregateNode); !ok {
				continue
			}
			for _, predicate := range plan.Predicates {
				if !nodeContainsPredicate(node, predicate) {
					return false, nil
				}
			}
		}
		plan.Predicates = nil
		return true, nil
	}
	for i, node := range plan.Nodes {
		sourceNode, ok := node.(semanticplan.SourceAggregateNode)
		if !ok {
			continue
		}
		base := sourceNode.Base
		if base.PredicateBoundaryEvidence.Movement != semanticplan.SemanticPredicateBoundaryAllowedWithProof || base.PredicateBoundaryEvidence.Proof != semanticplan.SemanticPredicateProofDatasetReachability {
			return false, serrors.Internal("source node does not permit predicate movement with dataset-reachability proof", map[string]any{"node": base.ID})
		}
		required := stringSet(sourceNode.Source.RequiredDatasets)
		for _, predicate := range plan.Predicates {
			if _, ok := required[predicate.Dataset]; !ok {
				return false, serrors.Internal("predicate dataset is not reachable from its source node", map[string]any{"dataset": predicate.Dataset, "node": base.ID})
			}
			if nodeContainsPredicate(sourceNode, predicate) {
				continue
			}
			owned := predicate
			base.Predicates = append(base.Predicates, semanticplan.SemanticPlanNodePredicate{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: base.ID, Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &owned})
		}
		sourceNode.Base = base
		plan.Nodes[i] = sourceNode
	}
	plan.Predicates = nil
	return true, nil
}

func (rule PredicatePushdownRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// JoinDeduplicationRule removes duplicate logical joins and refreshes the
// population-preservation evidence that is tied to the retained join set.
type JoinDeduplicationRule struct{}

func (JoinDeduplicationRule) Name() string { return "join_deduplication" }

func (JoinDeduplicationRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	changed := false
	plan.Joins, changed = deduplicateJoins(plan.Joins)
	if !semanticplan.RequiresComposedPlan(plan) {
		return changed, nil
	}
	for i, node := range plan.Nodes {
		source, ok := semanticplan.NodeSourceState(node)
		if !ok {
			continue
		}
		joins, didChange := deduplicateJoins(source.Joins)
		if !didChange {
			continue
		}
		source.Joins = joins
		source.PopulationPreservationEvidence = filterPopulationPreservationEvidence(source.PopulationPreservationEvidence, source.Joins)
		updated, err := semanticplan.WithNodeSourceState(node, source)
		if err != nil {
			return false, err
		}
		plan.Nodes[i] = updated
		changed = true
	}
	return changed, nil
}

func (rule JoinDeduplicationRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

// UnusedJoinEliminationRule removes joins whose destination datasets are not
// needed by the owned semantic output dependency closure.
type UnusedJoinEliminationRule struct{}

func (UnusedJoinEliminationRule) Name() string { return "unused_join_elimination" }

func (UnusedJoinEliminationRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	requirements, err := CollectRequirements(plan)
	if err != nil {
		return false, err
	}
	changed := false
	plan.Joins, changed = pruneJoinTree(plan.Joins, requirements.datasets)
	if !semanticplan.RequiresComposedPlan(plan) {
		return changed, nil
	}
	for i, node := range plan.Nodes {
		sourceNode, ok := node.(semanticplan.SourceAggregateNode)
		if !ok {
			continue
		}
		required := stringSet(sourceNode.Source.RequiredDatasets)
		joins, didChange := pruneJoinTree(sourceNode.Source.Joins, required)
		if !didChange {
			continue
		}
		sourceNode.Source.Joins = joins
		sourceNode.Source.PopulationPreservationEvidence = filterPopulationPreservationEvidence(sourceNode.Source.PopulationPreservationEvidence, sourceNode.Source.Joins)
		plan.Nodes[i] = sourceNode
		changed = true
	}
	return changed, nil
}

func (rule UnusedJoinEliminationRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}

func deduplicatePredicates(predicates []semanticplan.Predicate) ([]semanticplan.Predicate, bool) {
	out := make([]semanticplan.Predicate, 0, len(predicates))
	for _, predicate := range predicates {
		if !containsPredicate(out, predicate) {
			out = append(out, predicate)
		}
	}
	return out, len(out) != len(predicates)
}

func containsPredicate(predicates []semanticplan.Predicate, predicate semanticplan.Predicate) bool {
	for _, candidate := range predicates {
		if candidate.Dataset == predicate.Dataset && candidate.Expression == predicate.Expression && candidate.Filter.Field == predicate.Filter.Field && candidate.Filter.Operator == predicate.Filter.Operator && reflect.DeepEqual(candidate.Filter.Value, predicate.Filter.Value) {
			return true
		}
	}
	return false
}

func deduplicateJoins(joins []semanticplan.Join) ([]semanticplan.Join, bool) {
	seen := map[string]struct{}{}
	out := make([]semanticplan.Join, 0, len(joins))
	for _, join := range joins {
		key := relationshipKey(join.Relationship) + "|" + join.FromDataset + "|" + join.ToDataset
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, join)
	}
	return out, len(out) != len(joins)
}

func relationshipKey(rel *ossie.Relationship) string {
	if rel == nil {
		return ""
	}
	if rel.Name != "" {
		return rel.Name
	}
	return fmt.Sprintf("%s->%s", rel.From, rel.To)
}

func pruneJoinTree(joins []semanticplan.Join, required map[string]struct{}) ([]semanticplan.Join, bool) {
	keep := make([]bool, len(joins))
	for i := len(joins) - 1; i >= 0; i-- {
		join := joins[i]
		if _, ok := required[join.ToDataset]; !ok {
			continue
		}
		keep[i] = true
		required[join.FromDataset] = struct{}{}
	}
	out := make([]semanticplan.Join, 0, len(joins))
	for i := range joins {
		if keep[i] {
			out = append(out, joins[i])
		}
	}
	return out, len(out) != len(joins)
}

func nodeContainsPredicate(node semanticplan.SemanticPlanNode, predicate semanticplan.Predicate) bool {
	if node == nil {
		return false
	}
	for _, candidate := range node.NodeBase().Predicates {
		if candidate.Predicate != nil && containsPredicate([]semanticplan.Predicate{*candidate.Predicate}, predicate) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func normalizeStringSet(values []string) ([]string, bool) {
	if len(values) < 2 {
		return values, false
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			normalized = append(normalized, value)
		}
	}
	sort.Strings(normalized)
	if reflect.DeepEqual(values, normalized) {
		return values, false
	}
	return normalized, true
}

func New(rules ...Rule) *Optimizer { return &Optimizer{rules: append([]Rule(nil), rules...)} }

// NewCanonical configures semantic-set normalization before the caller's
// ordered rules, matching the canonical planner optimization contract.
func NewCanonical(rules ...Rule) *Optimizer {
	return NewWithPreRule(SemanticSetNormalizationRule{}, rules...)
}

// NewWithPreRule configures one canonicalization rule that runs before the
// fixed-point passes and is intentionally omitted from the optimization trace.
func NewWithPreRule(pre Rule, rules ...Rule) *Optimizer {
	return &Optimizer{preRule: pre, rules: append([]Rule(nil), rules...)}
}

// Default returns the canonical deterministic optimizer rule sequence.
func Default() *Optimizer {
	return NewWithPreRule(
		SemanticSetNormalizationRule{},
		MetricProjectionPruningRule{},
		SemanticSetNormalizationRule{},
		ProjectionDeduplicationRule{},
		PredicateDeduplicationRule{},
		PredicatePushdownRule{},
		JoinDeduplicationRule{},
		UnusedJoinEliminationRule{},
		SourceScanFusionRule{},
	)
}

// Rules returns an owned copy of the ordered rewrite set for the planner's
// graph-specific dispatch path.
func (o *Optimizer) Rules() []Rule {
	if o == nil {
		return nil
	}
	return append([]Rule(nil), o.rules...)
}

func (o *Optimizer) Optimize(ctx context.Context, input *semanticplan.SemanticPlan) (*semanticplan.SemanticPlan, error) {
	if input == nil {
		return nil, serrors.Internal("semantic plan is required", nil)
	}
	plan := semanticplan.ClonePlan(input)
	if o == nil {
		return plan, nil
	}
	if o.preRule != nil {
		if _, err := o.preRule.Apply(plan); err != nil {
			return nil, fmt.Errorf("canonicalize semantic plan: %w", err)
		}
	}
	for pass := 1; pass <= maxOptimizationPasses; pass++ {
		trace := make([]semanticplan.OptimizationStep, 0, len(o.rules))
		changed := false
		for _, rule := range o.rules {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if rule == nil {
				continue
			}
			didChange, err := rule.Apply(plan)
			if err != nil {
				return nil, fmt.Errorf("optimizer rule %s: %w", rule.Name(), err)
			}
			trace = append(trace, semanticplan.OptimizationStep{Rule: rule.Name(), Changed: didChange})
			changed = changed || didChange
		}
		if pass == 1 || changed {
			plan.OptimizationTrace = append(plan.OptimizationTrace, trace...)
		}
		if !changed {
			return plan, nil
		}
	}
	return nil, serrors.Internal("semantic plan optimizer did not converge", map[string]any{"max_passes": maxOptimizationPasses})
}

// OptimizeSemanticPlan applies only graph-aware rules through the bounded
// State contract. Generic Rule implementations are rejected rather than being
// given the complete SemanticPlan directly.
func (o *Optimizer) OptimizeSemanticPlan(ctx context.Context, input *semanticplan.SemanticPlan) (*semanticplan.SemanticPlan, error) {
	if input == nil || len(input.Nodes) == 0 {
		return nil, serrors.Internal("plan-owned semantic nodes are required for semantic plan optimization", nil)
	}
	plan := semanticplan.ClonePlan(input)
	state := NewState(plan)
	if o == nil {
		return plan, nil
	}
	if o.preRule != nil {
		if _, err := ApplySemanticRule(state, o.preRule); err != nil {
			return nil, fmt.Errorf("canonicalize semantic graph: %w", err)
		}
	}
	for pass := 1; pass <= maxOptimizationPasses; pass++ {
		trace := make([]semanticplan.OptimizationStep, 0, len(o.rules))
		changed := false
		for _, rule := range o.rules {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if rule == nil {
				continue
			}
			didChange, err := ApplySemanticRule(state, rule)
			if err != nil {
				return nil, fmt.Errorf("optimizer rule %s: %w", rule.Name(), err)
			}
			trace = append(trace, semanticplan.OptimizationStep{Rule: rule.Name(), Changed: didChange})
			changed = changed || didChange
		}
		if pass == 1 || changed {
			plan.OptimizationTrace = append(plan.OptimizationTrace, trace...)
		}
		if !changed {
			state.Write(plan)
			return plan, nil
		}
	}
	return nil, serrors.Internal("semantic plan DAG optimizer did not converge", map[string]any{"max_passes": maxOptimizationPasses})
}
