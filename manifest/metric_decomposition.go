package manifest

import (
	"math"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/expression"
)

// MetricDecompositionKind is the classification of how a metric's
// change can be decomposed across segments. It is derived from existing metric
// dependency and aggregation semantics; it is not model-authored metadata.
type MetricDecompositionKind string

const (
	MetricDecompositionAdditive    MetricDecompositionKind = "additive"
	MetricDecompositionRatio       MetricDecompositionKind = "ratio"
	MetricDecompositionUnsupported MetricDecompositionKind = "unsupported"
)

// MetricDecomposition is the closed manifest-level evidence used to determine
// which metric-change decompositions are supported.
type MetricDecomposition interface {
	Kind() MetricDecompositionKind
	metricDecomposition()
}

// AdditiveMetricDecomposition means independently grouped segment values can
// be merged with numeric addition to reproduce the metric total.
type AdditiveMetricDecomposition struct{}

func (AdditiveMetricDecomposition) Kind() MetricDecompositionKind { return MetricDecompositionAdditive }
func (AdditiveMetricDecomposition) metricDecomposition()          {}

// RatioMetricDecomposition records the canonical additive metric operands of a
// proven A/B metric. It admits only direct metric-reference
// ratios so numerator/denominator identity is unambiguous.
type RatioMetricDecomposition struct {
	Numerator   string
	Denominator string
}

func (RatioMetricDecomposition) Kind() MetricDecompositionKind { return MetricDecompositionRatio }
func (RatioMetricDecomposition) metricDecomposition()          {}

// UnsupportedMetricDecomposition is the fail-closed result when the current
// algebra cannot prove a safe decomposition.
type UnsupportedMetricDecomposition struct {
	Reason string
}

func (UnsupportedMetricDecomposition) Kind() MetricDecompositionKind {
	return MetricDecompositionUnsupported
}
func (UnsupportedMetricDecomposition) metricDecomposition() {}

// MetricDecomposition derives decomposition evidence for one metric.
// Multiple declared expression dialects must agree on the derived contract.
func (m *ModelIndex) MetricDecomposition(name string) MetricDecomposition {
	if m == nil {
		return unsupportedDecomposition("model index is nil")
	}
	return m.deriveMetricDecomposition(name, map[string]bool{})
}

func (m *ModelIndex) deriveMetricDecomposition(name string, visiting map[string]bool) MetricDecomposition {
	if visiting[name] {
		return unsupportedDecomposition("metric dependency cycle")
	}
	analysis, ok := m.MetricAnalysis(name)
	if !ok || len(analysis.Expressions) == 0 {
		return unsupportedDecomposition("metric analysis is not available")
	}

	visiting[name] = true
	defer delete(visiting, name)

	var result MetricDecomposition
	for _, analyzed := range analysis.Expressions {
		derived := m.deriveExpressionDecomposition(analyzed, visiting)
		if result == nil {
			result = derived
			continue
		}
		if !sameMetricDecomposition(result, derived) {
			return unsupportedDecomposition("declared expression dialects disagree on decomposition")
		}
	}
	if result == nil {
		return unsupportedDecomposition("metric analysis is not available")
	}
	return result
}

func (m *ModelIndex) deriveExpressionDecomposition(analyzed AnalyzedExpression, visiting map[string]bool) MetricDecomposition {
	if analyzed.Bound.Expr == nil {
		return unsupportedDecomposition("metric expression is empty")
	}
	cursor := aggregationEvidenceCursor{properties: analyzed.AggregationProperties}
	derived := m.deriveExpr(analyzed.Bound, analyzed.Bound.Expr, visiting, &cursor)
	if derived.Kind() != MetricDecompositionUnsupported && cursor.index != len(cursor.properties) {
		return unsupportedDecomposition("aggregation evidence was not fully consumed")
	}
	return derived
}

func (m *ModelIndex) deriveExpr(bound expression.BoundExpression, expr expression.Expr, visiting map[string]bool, cursor *aggregationEvidenceCursor) MetricDecomposition {
	switch node := expr.(type) {
	case *expression.IdentifierExpr:
		symbol, ok := bound.Resolve(node.Parts)
		if !ok || symbol.Kind != expression.BoundMetric {
			return unsupportedDecomposition("expression is not a metric reference")
		}
		return m.deriveMetricDecomposition(symbol.Name, visiting)

	case *expression.FunctionCallExpr:
		props, ok := cursor.consume(node.Name)
		if !ok || !isAdditiveAggregation(props) {
			return unsupportedDecomposition("aggregation is not additive")
		}
		return AdditiveMetricDecomposition{}

	case *expression.UnaryExpr:
		if node.Op != "+" && node.Op != "-" {
			return unsupportedDecomposition("unsupported unary metric expression")
		}
		child := m.deriveExpr(bound, node.Expr, visiting, cursor)
		if child.Kind() == MetricDecompositionAdditive {
			return child
		}
		return unsupportedDecomposition("unary expression operand is not additive")

	case *expression.BinaryExpr:
		return m.deriveBinaryExpr(bound, node, visiting, cursor)

	default:
		return unsupportedDecomposition("unsupported metric expression shape")
	}
}

func (m *ModelIndex) deriveBinaryExpr(bound expression.BoundExpression, node *expression.BinaryExpr, visiting map[string]bool, cursor *aggregationEvidenceCursor) MetricDecomposition {
	if node == nil {
		return unsupportedDecomposition("metric expression is empty")
	}
	left := m.deriveExpr(bound, node.Left, visiting, cursor)
	right := m.deriveExpr(bound, node.Right, visiting, cursor)

	switch node.Op {
	case "+", "-":
		if left.Kind() == MetricDecompositionAdditive && right.Kind() == MetricDecompositionAdditive {
			return AdditiveMetricDecomposition{}
		}
		return unsupportedDecomposition("addition or subtraction requires additive metric operands")

	case "*":
		if (isAdditive(left) && isNumericConstant(node.Right)) || (isNumericConstant(node.Left) && isAdditive(right)) {
			return AdditiveMetricDecomposition{}
		}
		return unsupportedDecomposition("multiplication is additive only by a numeric constant")

	case "/":
		if isAdditive(left) && isNonZeroNumericConstant(node.Right) {
			return AdditiveMetricDecomposition{}
		}
		numerator, numOK := directMetricReference(bound, node.Left)
		denominator, denOK := directMetricReference(bound, node.Right)
		if numOK && denOK && left.Kind() == MetricDecompositionAdditive && right.Kind() == MetricDecompositionAdditive {
			return RatioMetricDecomposition{Numerator: numerator, Denominator: denominator}
		}
		return unsupportedDecomposition("division is neither additive scaling nor a supported metric ratio")
	}
	return unsupportedDecomposition("unsupported binary metric operator")
}

// aggregationEvidenceCursor consumes the aggregation-algebra evidence recorded by the
// authoritative manifest analyzer in deterministic source order. Metric decomposition must
// not re-derive aggregation properties with a second registry or policy.
type aggregationEvidenceCursor struct {
	properties []expression.AggregationProperties
	index      int
}

func (c *aggregationEvidenceCursor) consume(function string) (expression.AggregationProperties, bool) {
	if c == nil || c.index >= len(c.properties) {
		return expression.AggregationProperties{}, false
	}
	props := c.properties[c.index]
	if !strings.EqualFold(strings.TrimSpace(props.Function), strings.TrimSpace(function)) {
		return expression.AggregationProperties{}, false
	}
	c.index++
	return props, true
}

func isAdditiveAggregation(props expression.AggregationProperties) bool {
	return props.Derived &&
		props.Rollup == expression.RollupDistributive &&
		strings.EqualFold(props.Merge, "SUM") &&
		props.Finalize == expression.FinalizeIdentity &&
		len(props.PartialState) == 1
}

func isAdditive(d MetricDecomposition) bool {
	return d != nil && d.Kind() == MetricDecompositionAdditive
}

func directMetricReference(bound expression.BoundExpression, expr expression.Expr) (string, bool) {
	identifier, ok := expr.(*expression.IdentifierExpr)
	if !ok {
		return "", false
	}
	symbol, ok := bound.Resolve(identifier.Parts)
	if !ok || symbol.Kind != expression.BoundMetric || strings.TrimSpace(symbol.Name) == "" {
		return "", false
	}
	return symbol.Name, true
}

func isNumericConstant(expr expression.Expr) bool {
	_, ok := numericConstant(expr)
	return ok
}

func isNonZeroNumericConstant(expr expression.Expr) bool {
	value, ok := numericConstant(expr)
	return ok && value != 0
}

func numericConstant(expr expression.Expr) (float64, bool) {
	literal, ok := expr.(*expression.LiteralExpr)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(literal.Value), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func unsupportedDecomposition(reason string) MetricDecomposition {
	return UnsupportedMetricDecomposition{Reason: reason}
}

func sameMetricDecomposition(left, right MetricDecomposition) bool {
	if left == nil || right == nil || left.Kind() != right.Kind() {
		return false
	}
	if left.Kind() != MetricDecompositionRatio {
		return true
	}
	l, lok := left.(RatioMetricDecomposition)
	r, rok := right.(RatioMetricDecomposition)
	return lok && rok && l.Numerator == r.Numerator && l.Denominator == r.Denominator
}
