package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
)

func cloneResolvedExpression(in expression.ResolvedExpression) expression.ResolvedExpression {
	return in.WithExtensionEvidence(in.ExtensionEvidenceValues()...)
}

func cloneConversionMetricSpec(in ossie.ConversionMetricSpec) ossie.ConversionMetricSpec {
	out := in
	if in.Window != nil {
		window := *in.Window
		out.Window = &window
	}
	out.ConstantProperties = append([]ossie.ConversionPropertyPair(nil), in.ConstantProperties...)
	return out
}

func cloneSemiAdditiveMetricSpec(in ossie.SemiAdditiveMetricSpec) ossie.SemiAdditiveMetricSpec {
	out := in
	out.WindowGroupings = append([]string(nil), in.WindowGroupings...)
	return out
}

// MetricEvaluationPlanExplanation is a bounded, source-independent projection
// of MetricEvaluationPlan. It deliberately contains no dataset, join, node,
// sharing, SQL, or cost information.
type MetricEvaluationPlanExplanation struct {
	Roots []MetricEvaluationRootExplanation `json:"roots"`
	Nodes []MetricEvaluationNodeExplanation `json:"nodes"`
}

type MetricEvaluationRootExplanation struct {
	Metric string               `json:"metric"`
	Role   MetricEvaluationRole `json:"role"`
}

type MetricEvaluationNodeExplanation struct {
	Metric     string               `json:"metric"`
	Kind       MetricEvaluationKind `json:"kind"`
	Inputs     []string             `json:"inputs,omitempty"`
	Dialect    string               `json:"dialect"`
	Expression string               `json:"expression"`
}

// CloneMetricEvaluationPlan returns an owned copy of plan. Resolver analysis
// objects are immutable by contract, while mutable slices, metric metadata,
// extension evidence, typed specs, and time bindings are copied.
func CloneMetricEvaluationPlan(plan *MetricEvaluationPlan) *MetricEvaluationPlan {
	if plan == nil {
		return nil
	}
	out := &MetricEvaluationPlan{
		Roots: cloneMetricEvaluationSlice(plan.Roots),
		Nodes: make([]MetricEvaluationNode, len(plan.Nodes)),
	}
	for i := range plan.Nodes {
		out.Nodes[i] = cloneMetricEvaluationNode(plan.Nodes[i])
	}
	return out
}

func cloneMetricEvaluationSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func cloneMetricEvaluationNode(in MetricEvaluationNode) MetricEvaluationNode {
	out := in
	out.Inputs = cloneMetricEvaluationSlice(in.Inputs)
	out.Metric = cloneMetricEvaluationMetric(in.Metric)
	out.Expression = cloneResolvedExpression(in.Expression)
	if in.TimeBinding != nil {
		binding := *in.TimeBinding
		out.TimeBinding = &binding
	}
	out.Spec = cloneMetricEvaluationSpec(in.Spec)
	return out
}

func cloneMetricEvaluationMetric(in *ossie.Metric) *ossie.Metric {
	if in == nil {
		return nil
	}
	out := *in
	out.CustomExtensions = cloneMetricEvaluationSlice(in.CustomExtensions)
	out.Expression.Dialects = cloneMetricEvaluationSlice(in.Expression.Dialects)
	out.AIContext = cloneMetricEvaluationJSONValue(in.AIContext)
	return &out
}

func cloneMetricEvaluationJSONValue(in any) any {
	switch value := in.(type) {
	case nil, string, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return value
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = cloneMetricEvaluationJSONValue(value[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = cloneMetricEvaluationJSONValue(item)
		}
		return out
	default:
		// Loader-owned AI context is JSON/YAML data. Unknown values are retained
		// rather than reflected into a new public cloning contract.
		return value
	}
}

func cloneMetricEvaluationSpec(in MetricEvaluationSpec) MetricEvaluationSpec {
	var out MetricEvaluationSpec
	if in.Source != nil {
		value := *in.Source
		out.Source = &value
	}
	if in.Derived != nil {
		value := *in.Derived
		out.Derived = &value
	}
	if in.Cumulative != nil {
		value := *in.Cumulative
		out.Cumulative = &value
	}
	if in.TimeOffset != nil {
		value := *in.TimeOffset
		out.TimeOffset = &value
	}
	if in.OffsetToGrain != nil {
		value := *in.OffsetToGrain
		out.OffsetToGrain = &value
	}
	if in.Conversion != nil {
		value := *in.Conversion
		value.Spec = cloneConversionMetricSpec(in.Conversion.Spec)
		out.Conversion = &value
	}
	if in.SemiAdditive != nil {
		value := *in.SemiAdditive
		value.Spec = cloneSemiAdditiveMetricSpec(in.SemiAdditive.Spec)
		out.SemiAdditive = &value
	}
	return out
}

// ExplainMetricEvaluationPlan returns a bounded explanation in canonical plan
// order. The output is safe to expose without implying source or physical SQL
// decisions that belong to later IRs.
func ExplainMetricEvaluationPlan(plan *MetricEvaluationPlan) (MetricEvaluationPlanExplanation, error) {
	if err := ValidateMetricEvaluationPlan(plan); err != nil {
		return MetricEvaluationPlanExplanation{}, err
	}
	explanation := MetricEvaluationPlanExplanation{
		Roots: make([]MetricEvaluationRootExplanation, len(plan.Roots)),
		Nodes: make([]MetricEvaluationNodeExplanation, len(plan.Nodes)),
	}
	for i, root := range plan.Roots {
		explanation.Roots[i] = MetricEvaluationRootExplanation{Metric: root.Metric, Role: root.Role}
	}
	for i, node := range plan.Nodes {
		inputs := make([]string, len(node.Inputs))
		for j, input := range node.Inputs {
			inputs[j] = input.Metric
		}
		explanation.Nodes[i] = MetricEvaluationNodeExplanation{
			Metric:     node.ID,
			Kind:       node.Kind,
			Inputs:     inputs,
			Dialect:    node.Expression.SourceDialect,
			Expression: node.Expression.Source,
		}
	}
	return explanation, nil
}

type metricEvaluationFingerprintProjection struct {
	Roots []MetricEvaluationRoot            `json:"roots"`
	Nodes []metricEvaluationFingerprintNode `json:"nodes"`
}

type metricEvaluationFingerprintNode struct {
	ID                string                       `json:"id"`
	Inputs            []string                     `json:"inputs,omitempty"`
	Kind              MetricEvaluationKind         `json:"kind"`
	ExpressionDialect string                       `json:"expression_dialect"`
	ExpressionSource  string                       `json:"expression_source"`
	TimeBinding       *ossie.MetricTimeBindingSpec `json:"time_binding,omitempty"`
	Spec              MetricEvaluationSpec         `json:"spec"`
	ExtensionEvidence []extension.Evidence         `json:"extension_evidence,omitempty"`
}

// FingerprintMetricEvaluationPlan computes a deterministic regression
// fingerprint of metric-evaluation meaning. It is not a public ID or cache key.
func FingerprintMetricEvaluationPlan(plan *MetricEvaluationPlan) (string, error) {
	if err := ValidateMetricEvaluationPlan(plan); err != nil {
		return "", err
	}
	projection := metricEvaluationFingerprintProjection{
		Roots: cloneMetricEvaluationSlice(plan.Roots),
		Nodes: make([]metricEvaluationFingerprintNode, len(plan.Nodes)),
	}
	sort.Slice(projection.Roots, func(i, j int) bool {
		if projection.Roots[i].Metric != projection.Roots[j].Metric {
			return projection.Roots[i].Metric < projection.Roots[j].Metric
		}
		return projection.Roots[i].Role < projection.Roots[j].Role
	})
	for i, node := range plan.Nodes {
		inputs := make([]string, len(node.Inputs))
		for j, input := range node.Inputs {
			inputs[j] = input.Metric
		}
		sort.Strings(inputs)
		projection.Nodes[i] = metricEvaluationFingerprintNode{
			ID:                node.ID,
			Inputs:            inputs,
			Kind:              node.Kind,
			ExpressionDialect: node.Expression.SourceDialect,
			ExpressionSource:  node.Expression.Source,
			TimeBinding:       node.TimeBinding,
			Spec:              cloneMetricEvaluationSpec(node.Spec),
			ExtensionEvidence: node.Expression.ExtensionEvidenceValues(),
		}
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("encode metric evaluation fingerprint projection: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
