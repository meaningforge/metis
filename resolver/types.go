package resolver

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

type ResolvedMetric struct {
	Name        string
	Metric      *ossie.Metric
	Datasets    []string
	Expression  expression.ResolvedExpression
	Cumulative  *ossie.CumulativeMetricSpec
	TimeBinding *ossie.MetricTimeBindingSpec
}

type ResolvedTimeSpine struct {
	Spec               ossie.TimeSpineSpec
	Dataset            *ossie.Dataset
	TimeField          *ossie.Field
	QueryTimeDimension string
	RequestedGrain     query.TimeGrain
	Expression         expression.ResolvedExpression
}

type ResolvedDimension struct {
	Name           string
	Grain          *query.TimeGrain
	Dataset        string
	Field          *ossie.Field
	Expression     expression.ResolvedExpression
	CustomCalendar *ResolvedCustomCalendarGrain
}

type FilterTargetKind string

const (
	FilterTargetField  FilterTargetKind = "field"
	FilterTargetMetric FilterTargetKind = "metric"
)

type ResolvedFilter struct {
	Filter         query.Filter
	Kind           FilterTargetKind
	Dataset        string
	Field          *ossie.Field
	Metric         *ossie.Metric
	MetricDatasets []string
	Expression     expression.ResolvedExpression
}
type OrderTargetKind string

const (
	OrderTargetMetric    OrderTargetKind = "metric"
	OrderTargetDimension OrderTargetKind = "dimension"
)

type ResolvedOrderBy struct {
	Name       string
	Direction  query.SortDirection
	Kind       OrderTargetKind
	Metric     *ossie.Metric
	Dataset    string
	Field      *ossie.Field
	Expression expression.ResolvedExpression
}

// SemanticQuerySpec is the canonical planner-facing request after semantic
// resolution. It contains request-specific evidence, never global topology,
// runtime placement, logical plan shape, or rendered SQL.
type SemanticQuerySpec struct {
	Project     string
	Model       *manifest.ModelIndex
	RootDataset string
	// Intent is the requested query intent, carried through so planning can
	// represent it as typed semantic state.
	//
	// It was validated here and then dropped, which left distinct_values and an
	// ordinary dimension query indistinguishable downstream of the resolver --
	// they compile to the same grouped read today. Semantic planning gives metric-free
	// queries a typed source-selection node whose mode is that distinction, so
	// the intent has to survive resolution to be represented at all.
	Intent            query.QueryIntent
	TimeSpine         *ResolvedTimeSpine
	Metrics           []ResolvedMetric
	EvaluationMetrics []ResolvedMetric
	Dimensions        []ResolvedDimension
	Filters           []ResolvedFilter
	Relationships     []*ossie.Relationship
	OrderBy           []ResolvedOrderBy
	FieldExpressions  map[string]expression.ResolvedExpression
	Limit             *int
}

func (q *SemanticQuerySpec) FieldExpression(dataset, name string) (expression.ResolvedExpression, bool) {
	if q == nil || q.FieldExpressions == nil {
		return expression.ResolvedExpression{}, false
	}
	resolved, ok := q.FieldExpressions[dataset+"."+name]
	return resolved, ok
}

func (q *SemanticQuerySpec) MetricExpression(name string) (expression.ResolvedExpression, bool) {
	if q == nil {
		return expression.ResolvedExpression{}, false
	}
	for _, metric := range q.EvaluationMetrics {
		if metric.Name == name {
			return metric.Expression, metric.Expression.IsResolved()
		}
	}
	for _, metric := range q.Metrics {
		if metric.Name == name {
			return metric.Expression, metric.Expression.IsResolved()
		}
	}
	return expression.ResolvedExpression{}, false
}

// ResolvedSemanticQuery is a source-compatible alias retained while callers
// migrate to SemanticQuerySpec. It is not a second query IR.
type ResolvedSemanticQuery = SemanticQuerySpec
