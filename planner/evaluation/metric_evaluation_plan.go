package evaluation

import (
	"fmt"
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/resolver"
)

func conversionDependenciesMatch(dependencies []string, spec ossie.ConversionMetricSpec) bool {
	if len(dependencies) != 2 {
		return false
	}
	seen := map[string]int{}
	for _, dependency := range dependencies {
		seen[dependency]++
	}
	return seen[spec.BaseMetric] == 1 && seen[spec.ConversionMetric] == 1
}

// MetricEvaluationPlan is the query-scoped, source-independent metric
// evaluation IR. Nodes are stored in deterministic
// topological order: every input of a node must appear earlier in Nodes.
//
// The plan owns metric identity, dependency, kind, and typed metric semantics.
// It deliberately owns no dataset roots, relationship paths, node grain,
// predicate placement, sharing groups, or SQL structure.
type MetricEvaluationPlan struct {
	Roots []MetricEvaluationRoot
	Nodes []MetricEvaluationNode
}

type MetricEvaluationRole string

const (
	MetricEvaluationRoleOutput    MetricEvaluationRole = "output"
	MetricEvaluationRolePredicate MetricEvaluationRole = "predicate"
	MetricEvaluationRoleOrder     MetricEvaluationRole = "order"
)

type MetricEvaluationRoot struct {
	Metric string
	Role   MetricEvaluationRole
}

type MetricEvaluationInput struct {
	Metric string
}

type MetricEvaluationKind string

const (
	MetricEvaluationSource        MetricEvaluationKind = "source"
	MetricEvaluationDerived       MetricEvaluationKind = "derived"
	MetricEvaluationCumulative    MetricEvaluationKind = "cumulative"
	MetricEvaluationTimeOffset    MetricEvaluationKind = "time_offset"
	MetricEvaluationOffsetToGrain MetricEvaluationKind = "offset_to_grain"
	MetricEvaluationConversion    MetricEvaluationKind = "conversion"
	MetricEvaluationSemiAdditive  MetricEvaluationKind = "semi_additive"
)

type SourceMetricEvaluationSpec struct{}
type DerivedMetricEvaluationSpec struct{}

type CumulativeMetricEvaluationSpec struct {
	Spec ossie.CumulativeMetricSpec
}

type TimeOffsetMetricEvaluationSpec struct {
	Spec ossie.TimeOffsetMetricSpec
}

type OffsetToGrainMetricEvaluationSpec struct {
	Spec ossie.OffsetToGrainMetricSpec
}

type ConversionMetricEvaluationSpec struct {
	Spec ossie.ConversionMetricSpec
}

type SemiAdditiveMetricEvaluationSpec struct {
	Spec ossie.SemiAdditiveMetricSpec
}

// MetricEvaluationSpec is a closed typed union. Exactly one field must be
// present, and it must match MetricEvaluationNode.Kind.
type MetricEvaluationSpec struct {
	Source        *SourceMetricEvaluationSpec
	Derived       *DerivedMetricEvaluationSpec
	Cumulative    *CumulativeMetricEvaluationSpec
	TimeOffset    *TimeOffsetMetricEvaluationSpec
	OffsetToGrain *OffsetToGrainMetricEvaluationSpec
	Conversion    *ConversionMetricEvaluationSpec
	SemiAdditive  *SemiAdditiveMetricEvaluationSpec
}

type MetricEvaluationNode struct {
	ID          string
	Inputs      []MetricEvaluationInput
	Metric      *ossie.Metric
	Expression  expression.ResolvedExpression
	TimeBinding *ossie.MetricTimeBindingSpec
	Kind        MetricEvaluationKind
	Spec        MetricEvaluationSpec
}

// BuildMetricEvaluationPlan materializes the resolver-owned metric evaluation
// closure as the query-scoped metric authority consumed by semantic lowering.
//
// Metric-free queries return nil, nil instead of fabricating a metric plan.
func BuildMetricEvaluationPlan(q *resolver.SemanticQuerySpec) (*MetricEvaluationPlan, error) {
	if q == nil {
		return nil, fmt.Errorf("resolved semantic query is nil")
	}
	if !RequiresMetricEvaluation(q) {
		return nil, nil
	}
	if q.Model == nil {
		return nil, fmt.Errorf("metric evaluation plan requires a resolved model")
	}

	plan := &MetricEvaluationPlan{
		Roots: buildMetricEvaluationRoots(q),
		Nodes: make([]MetricEvaluationNode, 0, len(q.EvaluationMetrics)),
	}
	for _, metric := range q.EvaluationMetrics {
		dependency, ok := q.Model.MetricDependency(metric.Name)
		if !ok {
			return nil, fmt.Errorf("metric %q has no dependency analysis", metric.Name)
		}
		node, err := buildMetricEvaluationNode(metric, dependency.Metrics)
		if err != nil {
			return nil, err
		}
		plan.Nodes = append(plan.Nodes, node)
	}
	if err := ValidateMetricEvaluationPlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// RequiresMetricEvaluation reports whether the resolved query has an output,
// metric-predicate, or metric-order obligation. It is the narrow companion to
// BuildMetricEvaluationPlan used by the semantic construction boundary to
// distinguish a valid metric-free bypass from a missing required plan.
func RequiresMetricEvaluation(q *resolver.SemanticQuerySpec) bool {
	if q == nil {
		return false
	}
	return len(buildMetricEvaluationRoots(q)) != 0
}

func buildMetricEvaluationRoots(q *resolver.SemanticQuerySpec) []MetricEvaluationRoot {
	seen := map[MetricEvaluationRoot]struct{}{}
	roots := make([]MetricEvaluationRoot, 0, len(q.Metrics)+len(q.Filters)+len(q.OrderBy))
	add := func(metric string, role MetricEvaluationRole) {
		if metric == "" {
			return
		}
		root := MetricEvaluationRoot{Metric: metric, Role: role}
		if _, ok := seen[root]; ok {
			return
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	for _, metric := range q.Metrics {
		add(metric.Name, MetricEvaluationRoleOutput)
	}
	for _, filter := range q.Filters {
		if filter.Kind == resolver.FilterTargetMetric && filter.Metric != nil {
			add(filter.Metric.Name, MetricEvaluationRolePredicate)
		}
	}
	for _, order := range q.OrderBy {
		if order.Kind == resolver.OrderTargetMetric && order.Metric != nil {
			add(order.Name, MetricEvaluationRoleOrder)
		}
	}
	return roots
}

func buildMetricEvaluationNode(metric resolver.ResolvedMetric, dependencies []string) (MetricEvaluationNode, error) {
	node := MetricEvaluationNode{
		ID:         metric.Name,
		Metric:     cloneMetricEvaluationMetric(metric.Metric),
		Expression: cloneResolvedExpression(metric.Expression),
		Inputs:     make([]MetricEvaluationInput, len(dependencies)),
	}
	for i, dependency := range dependencies {
		node.Inputs[i] = MetricEvaluationInput{Metric: dependency}
	}
	if metric.TimeBinding != nil {
		binding := *metric.TimeBinding
		node.TimeBinding = &binding
	}

	cumulative, hasCumulative, err := ossie.CumulativeSpec(metric.Metric)
	if err != nil {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q: invalid cumulative metric semantics: %w", metric.Name, err)
	}
	timeOffset, hasTimeOffset, err := ossie.TimeOffsetSpec(metric.Metric)
	if err != nil {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q: invalid time-offset metric semantics: %w", metric.Name, err)
	}
	offsetToGrain, hasOffsetToGrain, err := ossie.OffsetToGrainSpec(metric.Metric)
	if err != nil {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q: invalid offset-to-grain metric semantics: %w", metric.Name, err)
	}
	conversion, hasConversion, err := ossie.ConversionSpec(metric.Metric)
	if err != nil {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q: invalid conversion metric semantics: %w", metric.Name, err)
	}
	semiAdditive, hasSemiAdditive, err := ossie.SemiAdditiveSpec(metric.Metric)
	if err != nil {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q: invalid semi-additive metric semantics: %w", metric.Name, err)
	}

	advanced := 0
	for _, present := range []bool{hasCumulative, hasTimeOffset, hasOffsetToGrain, hasConversion, hasSemiAdditive} {
		if present {
			advanced++
		}
	}
	if advanced > 1 {
		return MetricEvaluationNode{}, fmt.Errorf("metric %q has multiple semantic evaluation extensions", metric.Name)
	}

	switch {
	case hasCumulative:
		node.Kind = MetricEvaluationCumulative
		node.Spec.Cumulative = &CumulativeMetricEvaluationSpec{Spec: cumulative}
	case hasTimeOffset:
		node.Kind = MetricEvaluationTimeOffset
		node.Spec.TimeOffset = &TimeOffsetMetricEvaluationSpec{Spec: timeOffset}
	case hasOffsetToGrain:
		node.Kind = MetricEvaluationOffsetToGrain
		node.Spec.OffsetToGrain = &OffsetToGrainMetricEvaluationSpec{Spec: offsetToGrain}
	case hasConversion:
		node.Kind = MetricEvaluationConversion
		node.Spec.Conversion = &ConversionMetricEvaluationSpec{Spec: cloneConversionMetricSpec(conversion)}
	case hasSemiAdditive:
		node.Kind = MetricEvaluationSemiAdditive
		node.Spec.SemiAdditive = &SemiAdditiveMetricEvaluationSpec{Spec: cloneSemiAdditiveMetricSpec(semiAdditive)}
	case len(dependencies) == 0:
		node.Kind = MetricEvaluationSource
		node.Spec.Source = &SourceMetricEvaluationSpec{}
	default:
		node.Kind = MetricEvaluationDerived
		node.Spec.Derived = &DerivedMetricEvaluationSpec{}
	}
	return node, nil
}

// ValidateMetricEvaluationPlan validates the query-scoped metric dependency DAG
// without consulting source/node planning state.
func ValidateMetricEvaluationPlan(plan *MetricEvaluationPlan) error {
	if plan == nil {
		return fmt.Errorf("metric evaluation plan is nil")
	}
	if len(plan.Roots) == 0 {
		return fmt.Errorf("metric evaluation plan has no roots")
	}
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("metric evaluation plan has no nodes")
	}

	rootSeen := map[MetricEvaluationRoot]struct{}{}
	for _, root := range plan.Roots {
		if root.Metric == "" {
			return fmt.Errorf("metric evaluation root has empty metric")
		}
		if !validMetricEvaluationRole(root.Role) {
			return fmt.Errorf("metric evaluation root %q has unsupported role %q", root.Metric, root.Role)
		}
		if _, ok := rootSeen[root]; ok {
			return fmt.Errorf("duplicate metric evaluation root %q with role %q", root.Metric, root.Role)
		}
		rootSeen[root] = struct{}{}
	}

	positions := make(map[string]int, len(plan.Nodes))
	for i := range plan.Nodes {
		node := &plan.Nodes[i]
		if node.ID == "" {
			return fmt.Errorf("metric evaluation node %d has empty id", i)
		}
		if _, exists := positions[node.ID]; exists {
			return fmt.Errorf("duplicate metric evaluation node %q", node.ID)
		}
		positions[node.ID] = i
		if node.Metric == nil {
			return fmt.Errorf("metric evaluation node %q has no metric", node.ID)
		}
		if node.Metric.Name != node.ID {
			return fmt.Errorf("metric evaluation node %q carries metric %q", node.ID, node.Metric.Name)
		}
		if !node.Expression.IsResolved() {
			return fmt.Errorf("metric evaluation node %q has no target-resolved expression", node.ID)
		}
		if node.Expression.Analysis == nil {
			return fmt.Errorf("metric evaluation node %q has no resolved semantic analysis", node.ID)
		}
		if node.TimeBinding != nil {
			if err := ossie.ValidateMetricTimeBinding(*node.TimeBinding); err != nil {
				return fmt.Errorf("metric evaluation node %q has invalid time binding: %w", node.ID, err)
			}
		}
		if err := validateMetricEvaluationSpec(node.Kind, node.Spec); err != nil {
			return fmt.Errorf("metric evaluation node %q: %w", node.ID, err)
		}
		inputSeen := make(map[string]struct{}, len(node.Inputs))
		for _, input := range node.Inputs {
			if input.Metric == "" {
				return fmt.Errorf("metric evaluation node %q has empty input", node.ID)
			}
			if _, exists := inputSeen[input.Metric]; exists {
				return fmt.Errorf("metric evaluation node %q has duplicate input %q", node.ID, input.Metric)
			}
			inputSeen[input.Metric] = struct{}{}
			inputPosition, ok := positions[input.Metric]
			if !ok || inputPosition >= i {
				return fmt.Errorf("metric evaluation node %q input %q is not an earlier node", node.ID, input.Metric)
			}
		}
		if err := validateMetricEvaluationDependencyContract(*node); err != nil {
			return fmt.Errorf("metric evaluation node %q: %w", node.ID, err)
		}
	}

	for _, root := range plan.Roots {
		if _, ok := positions[root.Metric]; !ok {
			return fmt.Errorf("metric evaluation root %q has no node", root.Metric)
		}
	}
	if err := validateMetricEvaluationReachability(plan, positions); err != nil {
		return err
	}
	return nil
}

func validMetricEvaluationRole(role MetricEvaluationRole) bool {
	switch role {
	case MetricEvaluationRoleOutput, MetricEvaluationRolePredicate, MetricEvaluationRoleOrder:
		return true
	default:
		return false
	}
}

func validateMetricEvaluationSpec(kind MetricEvaluationKind, spec MetricEvaluationSpec) error {
	variants := 0
	for _, present := range []bool{
		spec.Source != nil,
		spec.Derived != nil,
		spec.Cumulative != nil,
		spec.TimeOffset != nil,
		spec.OffsetToGrain != nil,
		spec.Conversion != nil,
		spec.SemiAdditive != nil,
	} {
		if present {
			variants++
		}
	}
	if variants != 1 {
		return fmt.Errorf("kind %q must carry exactly one typed spec, got %d", kind, variants)
	}

	switch kind {
	case MetricEvaluationSource:
		if spec.Source == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
	case MetricEvaluationDerived:
		if spec.Derived == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
	case MetricEvaluationCumulative:
		if spec.Cumulative == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
		if err := ossie.ValidateCumulativeSpec(spec.Cumulative.Spec); err != nil {
			return fmt.Errorf("invalid cumulative spec: %w", err)
		}
	case MetricEvaluationTimeOffset:
		if spec.TimeOffset == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
		if err := ossie.ValidateTimeOffsetSpec(spec.TimeOffset.Spec); err != nil {
			return fmt.Errorf("invalid time-offset spec: %w", err)
		}
	case MetricEvaluationOffsetToGrain:
		if spec.OffsetToGrain == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
		if err := ossie.ValidateOffsetToGrainSpec(spec.OffsetToGrain.Spec); err != nil {
			return fmt.Errorf("invalid offset-to-grain spec: %w", err)
		}
	case MetricEvaluationConversion:
		if spec.Conversion == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
		if err := ossie.ValidateConversionSpec(spec.Conversion.Spec); err != nil {
			return fmt.Errorf("invalid conversion spec: %w", err)
		}
	case MetricEvaluationSemiAdditive:
		if spec.SemiAdditive == nil {
			return fmt.Errorf("typed spec does not match kind %q", kind)
		}
		if err := ossie.ValidateSemiAdditiveSpec(spec.SemiAdditive.Spec); err != nil {
			return fmt.Errorf("invalid semi-additive spec: %w", err)
		}
	default:
		return fmt.Errorf("unsupported metric evaluation kind %q", kind)
	}
	return nil
}

func validateMetricEvaluationDependencyContract(node MetricEvaluationNode) error {
	inputs := make([]string, len(node.Inputs))
	for i, input := range node.Inputs {
		inputs[i] = input.Metric
	}

	switch node.Kind {
	case MetricEvaluationSource:
		if len(inputs) != 0 {
			return fmt.Errorf("source metric has metric inputs %v", inputs)
		}
	case MetricEvaluationDerived:
		if len(inputs) == 0 {
			return fmt.Errorf("derived metric has no metric inputs")
		}
	case MetricEvaluationCumulative:
		if len(inputs) != 1 || inputs[0] != node.Spec.Cumulative.Spec.BaseMetric {
			return fmt.Errorf("cumulative dependency %v does not match base metric %q", inputs, node.Spec.Cumulative.Spec.BaseMetric)
		}
	case MetricEvaluationTimeOffset:
		if len(inputs) != 1 || inputs[0] != node.Spec.TimeOffset.Spec.BaseMetric {
			return fmt.Errorf("time-offset dependency %v does not match base metric %q", inputs, node.Spec.TimeOffset.Spec.BaseMetric)
		}
	case MetricEvaluationOffsetToGrain:
		if len(inputs) != 1 || inputs[0] != node.Spec.OffsetToGrain.Spec.BaseMetric {
			return fmt.Errorf("offset-to-grain dependency %v does not match base metric %q", inputs, node.Spec.OffsetToGrain.Spec.BaseMetric)
		}
	case MetricEvaluationConversion:
		if !conversionDependenciesMatch(inputs, node.Spec.Conversion.Spec) {
			return fmt.Errorf("conversion dependencies %v do not match base %q and conversion %q", inputs, node.Spec.Conversion.Spec.BaseMetric, node.Spec.Conversion.Spec.ConversionMetric)
		}
	case MetricEvaluationSemiAdditive:
		if len(inputs) != 1 || inputs[0] != node.Spec.SemiAdditive.Spec.BaseMetric {
			return fmt.Errorf("semi-additive dependency %v does not match base metric %q", inputs, node.Spec.SemiAdditive.Spec.BaseMetric)
		}
	}
	return nil
}

func validateMetricEvaluationReachability(plan *MetricEvaluationPlan, positions map[string]int) error {
	reachable := make(map[string]struct{}, len(plan.Nodes))
	var visit func(string)
	visit = func(metric string) {
		if _, ok := reachable[metric]; ok {
			return
		}
		reachable[metric] = struct{}{}
		node := plan.Nodes[positions[metric]]
		for _, input := range node.Inputs {
			visit(input.Metric)
		}
	}
	for _, root := range plan.Roots {
		visit(root.Metric)
	}
	if len(reachable) == len(plan.Nodes) {
		return nil
	}
	unreachable := make([]string, 0, len(plan.Nodes)-len(reachable))
	for _, node := range plan.Nodes {
		if _, ok := reachable[node.ID]; !ok {
			unreachable = append(unreachable, node.ID)
		}
	}
	sort.Strings(unreachable)
	return fmt.Errorf("metric evaluation plan has unreachable nodes %v", unreachable)
}
