package manifest

import (
	"testing"

	"github.com/meaningforge/metis/expression"
)

func TestMetricDecompositionDirectAggregations(t *testing.T) {
	tests := []struct {
		name string
		expr expression.Expr
		want MetricDecompositionKind
	}{
		{name: "sum", expr: call("SUM", ident("amount")), want: MetricDecompositionAdditive},
		{name: "count", expr: call("COUNT", &expression.WildcardExpr{}), want: MetricDecompositionAdditive},
		{name: "sumif", expr: call("SUMIF", ident("amount"), ident("paid")), want: MetricDecompositionAdditive},
		{name: "min", expr: call("MIN", ident("amount")), want: MetricDecompositionUnsupported},
		{name: "max", expr: call("MAX", ident("amount")), want: MetricDecompositionUnsupported},
		{name: "avg", expr: call("AVG", ident("amount")), want: MetricDecompositionUnsupported},
		{name: "count distinct", expr: call("COUNT", &expression.UnaryExpr{Op: "DISTINCT", Expr: ident("customer_id")}), want: MetricDecompositionUnsupported},
		{name: "unknown aggregate", expr: call("uniqExact", ident("customer_id")), want: MetricDecompositionUnsupported},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := modelWithAnalyses(map[string]MetricAnalysis{
				"metric": metricAnalysis(tt.expr, nil),
			})
			if got := model.MetricDecomposition("metric").Kind(); got != tt.want {
				t.Fatalf("MetricDecomposition() kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetricDecompositionDerivedLinearMetrics(t *testing.T) {
	model := modelWithAnalyses(map[string]MetricAnalysis{
		"revenue": metricAnalysis(call("SUM", ident("amount")), nil),
		"cost":    metricAnalysis(call("SUM", ident("cost")), nil),
		"margin": metricAnalysis(
			&expression.BinaryExpr{Left: ident("revenue"), Op: "-", Right: ident("cost")},
			metricBindings("revenue", "cost"),
		),
		"scaled": metricAnalysis(
			&expression.BinaryExpr{Left: literal("2"), Op: "*", Right: ident("revenue")},
			metricBindings("revenue"),
		),
		"divided": metricAnalysis(
			&expression.BinaryExpr{Left: ident("revenue"), Op: "/", Right: literal("2")},
			metricBindings("revenue"),
		),
		"offset": metricAnalysis(
			&expression.BinaryExpr{Left: ident("revenue"), Op: "+", Right: literal("1")},
			metricBindings("revenue"),
		),
		"nonlinear": metricAnalysis(
			&expression.BinaryExpr{Left: ident("revenue"), Op: "*", Right: ident("cost")},
			metricBindings("revenue", "cost"),
		),
	})

	for _, name := range []string{"margin", "scaled", "divided"} {
		if got := model.MetricDecomposition(name).Kind(); got != MetricDecompositionAdditive {
			t.Errorf("%s decomposition = %q, want additive", name, got)
		}
	}
	for _, name := range []string{"offset", "nonlinear"} {
		if got := model.MetricDecomposition(name).Kind(); got != MetricDecompositionUnsupported {
			t.Errorf("%s decomposition = %q, want unsupported", name, got)
		}
	}
}

func TestMetricDecompositionRatioRequiresDirectAdditiveMetricOperands(t *testing.T) {
	model := modelWithAnalyses(map[string]MetricAnalysis{
		"revenue":     metricAnalysis(call("SUM", ident("amount")), nil),
		"order_count": metricAnalysis(call("COUNT", &expression.WildcardExpr{}), nil),
		"aov": metricAnalysis(
			&expression.BinaryExpr{Left: ident("revenue"), Op: "/", Right: ident("order_count")},
			metricBindings("revenue", "order_count"),
		),
	})

	got := model.MetricDecomposition("aov")
	if got.Kind() != MetricDecompositionRatio {
		t.Fatalf("aov decomposition = %q, want ratio", got.Kind())
	}
	ratio, ok := got.(RatioMetricDecomposition)
	if !ok {
		t.Fatalf("aov decomposition type = %T, want RatioMetricDecomposition", got)
	}
	if ratio.Numerator != "revenue" || ratio.Denominator != "order_count" {
		t.Fatalf("ratio operands = %s/%s, want revenue/order_count", ratio.Numerator, ratio.Denominator)
	}
}

func TestMetricDecompositionFailsClosedWhenDialectsDisagree(t *testing.T) {
	sumExpr := call("SUM", ident("amount"))
	avgExpr := call("AVG", ident("amount"))
	model := modelWithAnalyses(map[string]MetricAnalysis{
		"metric": {
			Expressions: []AnalyzedExpression{
				analyzedExpression("ANSI_SQL", sumExpr, nil),
				analyzedExpression("CLICKHOUSE", avgExpr, nil),
			},
		},
	})

	got := model.MetricDecomposition("metric")
	if got.Kind() != MetricDecompositionUnsupported {
		t.Fatalf("decomposition = %q, want unsupported", got.Kind())
	}
}

func TestMetricDecompositionConsumesStoredAggregationEvidence(t *testing.T) {
	expr := call("SUM", ident("amount"))
	analyzed := analyzedExpression("ANSI_SQL", expr, nil)
	analyzed.AggregationProperties = []expression.AggregationProperties{
		expression.UnknownAggregationProperties("SUM"),
	}
	model := modelWithAnalyses(map[string]MetricAnalysis{
		"metric": {Expressions: []AnalyzedExpression{analyzed}},
	})

	if got := model.MetricDecomposition("metric").Kind(); got != MetricDecompositionUnsupported {
		t.Fatalf("decomposition = %q, want unsupported from stored fail-closed evidence", got)
	}
}

func TestMetricDecompositionFailsClosedForMissingAndCyclicAnalysis(t *testing.T) {
	model := modelWithAnalyses(map[string]MetricAnalysis{
		"a": metricAnalysis(ident("b"), metricBindingsNamed(map[string]string{"b": "b"})),
		"b": metricAnalysis(ident("a"), metricBindingsNamed(map[string]string{"a": "a"})),
	})

	if got := model.MetricDecomposition("missing").Kind(); got != MetricDecompositionUnsupported {
		t.Fatalf("missing metric decomposition = %q, want unsupported", got)
	}
	if got := model.MetricDecomposition("a").Kind(); got != MetricDecompositionUnsupported {
		t.Fatalf("cyclic metric decomposition = %q, want unsupported", got)
	}
}

func modelWithAnalyses(analyses map[string]MetricAnalysis) *ModelIndex {
	return &ModelIndex{MetricAnalyses: analyses}
}

func metricAnalysis(expr expression.Expr, bindings map[expression.Reference]expression.BoundSymbol) MetricAnalysis {
	return MetricAnalysis{Expressions: []AnalyzedExpression{analyzedExpression("ANSI_SQL", expr, bindings)}}
}

func analyzedExpression(dialect string, expr expression.Expr, bindings map[expression.Reference]expression.BoundSymbol) AnalyzedExpression {
	if bindings == nil {
		bindings = map[expression.Reference]expression.BoundSymbol{}
	}
	return AnalyzedExpression{
		Dialect:               dialect,
		Bound:                 expression.BoundExpression{Expr: expr, Bindings: bindings},
		AggregationProperties: expression.CollectAggregationProperties(expr, expression.DefaultFunctionRegistry()),
	}
}

func metricBindings(names ...string) map[expression.Reference]expression.BoundSymbol {
	mapping := make(map[string]string, len(names))
	for _, name := range names {
		mapping[name] = name
	}
	return metricBindingsNamed(mapping)
}

func metricBindingsNamed(names map[string]string) map[expression.Reference]expression.BoundSymbol {
	bindings := make(map[expression.Reference]expression.BoundSymbol, len(names))
	for ref, metric := range names {
		bindings[expression.Reference{Name: ref}] = expression.BoundSymbol{Kind: expression.BoundMetric, Name: metric}
	}
	return bindings
}

func ident(name string) expression.Expr {
	return &expression.IdentifierExpr{Parts: []string{name}}
}

func literal(value string) expression.Expr {
	return &expression.LiteralExpr{Value: value}
}

func call(name string, args ...expression.Expr) expression.Expr {
	return &expression.FunctionCallExpr{Name: name, Args: args}
}
