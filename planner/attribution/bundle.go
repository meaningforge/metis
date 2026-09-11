package attribution

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

// MetricAttributionDimensionPlan is one independent decomposition query in an
// attribution bundle. Dimension is routing identity; Plan remains the complete
// semantic authority for that one query.
type MetricAttributionDimensionPlan struct {
	Dimension string
	Plan      *semanticplan.SemanticPlan
}

// MetricAttributionBundle shares request semantics while retaining one
// independent SemanticPlan per decomposition dimension. It is an internal
// compiler contract and owns all request and plan state it exposes.
type MetricAttributionBundle struct {
	Request ResolvedMetricAttributionRequest
	Queries []MetricAttributionDimensionPlan
}

// BuildMetricAttributionBundle validates that plans exactly cover the bounded
// request and agree on its shared metric, periods, filters, and model.
// Query order is the canonical dimension-ref order, independent of caller
// order. It performs no manifest lookup and does not combine dimensions into one
// SemanticPlan.
func BuildMetricAttributionBundle(request ResolvedMetricAttributionRequest, queries []MetricAttributionDimensionPlan) (*MetricAttributionBundle, error) {
	if err := ValidateResolvedMetricAttributionRequest(request); err != nil {
		return nil, err
	}
	if len(queries) != len(request.Dimensions) {
		return nil, fmt.Errorf("metric attribution bundle requires exactly one semantic plan for each resolved dimension")
	}

	request = canonicalMetricAttributionRequest(request)
	requested := make(map[string]struct{}, len(request.Dimensions))
	for _, dimension := range request.Dimensions {
		requested[dimension] = struct{}{}
	}

	owned := make([]MetricAttributionDimensionPlan, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	var model semanticplan.ModelRef
	var sourcePredicates []semanticplan.Predicate
	var decomposition string
	var numeratorRef string
	var denominatorRef string
	for _, query := range queries {
		if _, ok := requested[query.Dimension]; !ok {
			return nil, fmt.Errorf("metric attribution bundle dimension %q is not present in the resolved request", query.Dimension)
		}
		if _, exists := seen[query.Dimension]; exists {
			return nil, fmt.Errorf("metric attribution bundle contains duplicate plan for dimension %q", query.Dimension)
		}
		seen[query.Dimension] = struct{}{}
		if query.Plan == nil {
			return nil, fmt.Errorf("metric attribution bundle dimension %q requires a semantic plan", query.Dimension)
		}
		if err := semanticplan.ValidateSemanticPlan(query.Plan); err != nil {
			return nil, fmt.Errorf("metric attribution bundle dimension %q has invalid semantic plan: %w", query.Dimension, err)
		}
		evidence, err := metricAttributionBundleEvidenceOf(query.Plan)
		if err != nil {
			return nil, fmt.Errorf("metric attribution bundle dimension %q: %w", query.Dimension, err)
		}
		if evidence.DimensionRef != query.Dimension || evidence.MetricRef != request.MetricRef || evidence.TimeDimensionRef != request.TimeDimensionRef || !sameMetricAttributionRange(evidence.Baseline, request.Baseline) || !sameMetricAttributionRange(evidence.Current, request.Current) {
			return nil, fmt.Errorf("metric attribution bundle dimension %q does not match the shared resolved request", query.Dimension)
		}
		if query.Plan.Model.Project != request.ProjectID {
			return nil, fmt.Errorf("metric attribution bundle dimension %q belongs to project %q, want %q", query.Dimension, query.Plan.Model.Project, request.ProjectID)
		}
		if !semanticplan.SamePredicateSet(query.Plan.Predicates, request.Filters) || !predicateSetContains(evidence.SourceWork.Predicates, request.Filters) {
			return nil, fmt.Errorf("metric attribution bundle dimension %q does not preserve the shared filters", query.Dimension)
		}
		if len(owned) == 0 {
			model = query.Plan.Model
			sourcePredicates = append([]semanticplan.Predicate(nil), evidence.SourceWork.Predicates...)
			decomposition = evidence.Decomposition
			numeratorRef = evidence.NumeratorRef
			denominatorRef = evidence.DenominatorRef
		} else if query.Plan.Model != model {
			return nil, fmt.Errorf("metric attribution bundle dimensions must resolve to one semantic model")
		} else if evidence.Decomposition != decomposition || evidence.NumeratorRef != numeratorRef || evidence.DenominatorRef != denominatorRef {
			return nil, fmt.Errorf("metric attribution bundle dimensions must use one decomposition proof")
		} else if !semanticplan.SamePredicateSet(evidence.SourceWork.Predicates, sourcePredicates) {
			return nil, fmt.Errorf("metric attribution bundle dimensions must use one governed source-filter set")
		}
		owned = append(owned, MetricAttributionDimensionPlan{Dimension: query.Dimension, Plan: semanticplan.ClonePlan(query.Plan)})
	}

	sort.Slice(owned, func(i, j int) bool { return owned[i].Dimension < owned[j].Dimension })
	return &MetricAttributionBundle{Request: request, Queries: owned}, nil
}

type metricAttributionBundleEvidence struct {
	MetricRef        string
	TimeDimensionRef string
	DimensionRef     string
	Baseline         semanticplan.MetricAttributionTimeRange
	Current          semanticplan.MetricAttributionTimeRange
	Decomposition    string
	NumeratorRef     string
	DenominatorRef   string
	SourceWork       semanticplan.SourceScanWork
}

func metricAttributionBundleEvidenceOf(plan *semanticplan.SemanticPlan) (metricAttributionBundleEvidence, error) {
	additive, ratio := attributionNodes(plan)
	if (additive == nil) == (ratio == nil) {
		return metricAttributionBundleEvidence{}, fmt.Errorf("metric attribution bundle requires exactly one supported attribution node")
	}
	if additive != nil {
		producer, err := sourceAggregateByID(plan, additive.Base.Inputs[0].NodeID)
		if err != nil {
			return metricAttributionBundleEvidence{}, err
		}
		work, err := semanticplan.SourceScanWorkOfNode(producer)
		if err != nil {
			return metricAttributionBundleEvidence{}, fmt.Errorf("cannot resolve shared additive source work: %w", err)
		}
		return metricAttributionBundleEvidence{
			MetricRef:        additive.MetricRef,
			TimeDimensionRef: additive.TimeDimensionRef,
			DimensionRef:     additive.DimensionRef,
			Baseline:         additive.Baseline,
			Current:          additive.Current,
			Decomposition:    additiveAttributionDecompositionKind(*additive),
			SourceWork:       work,
		}, nil
	}

	numerator, err := sourceAggregateByID(plan, ratio.NumeratorRef)
	if err != nil {
		return metricAttributionBundleEvidence{}, err
	}
	denominator, err := sourceAggregateByID(plan, ratio.DenominatorRef)
	if err != nil {
		return metricAttributionBundleEvidence{}, err
	}
	work, err := semanticplan.SourceScanWorkOfNode(numerator)
	if err != nil {
		return metricAttributionBundleEvidence{}, fmt.Errorf("cannot resolve shared ratio source work: %w", err)
	}
	otherWork, err := semanticplan.SourceScanWorkOfNode(denominator)
	if err != nil {
		return metricAttributionBundleEvidence{}, fmt.Errorf("cannot resolve shared ratio source work: %w", err)
	}
	identity, err := work.Identity()
	if err != nil {
		return metricAttributionBundleEvidence{}, err
	}
	otherIdentity, err := otherWork.Identity()
	if err != nil {
		return metricAttributionBundleEvidence{}, err
	}
	if identity != otherIdentity {
		return metricAttributionBundleEvidence{}, fmt.Errorf("ratio attribution operands must use one governed source population")
	}
	return metricAttributionBundleEvidence{
		MetricRef:        ratio.MetricRef,
		TimeDimensionRef: ratio.TimeDimensionRef,
		DimensionRef:     ratio.DimensionRef,
		Baseline:         ratio.Baseline,
		Current:          ratio.Current,
		Decomposition:    ratioAttributionDecompositionKind(*ratio),
		NumeratorRef:     ratio.NumeratorRef,
		DenominatorRef:   ratio.DenominatorRef,
		SourceWork:       work,
	}, nil
}

func attributionNodes(plan *semanticplan.SemanticPlan) (*semanticplan.AdditiveAttributionNode, *semanticplan.RatioAttributionNode) {
	var additive *semanticplan.AdditiveAttributionNode
	var ratio *semanticplan.RatioAttributionNode
	for _, node := range plan.Nodes {
		switch node := node.(type) {
		case semanticplan.AdditiveAttributionNode:
			if additive != nil {
				return nil, nil
			}
			copy := node
			additive = &copy
		case semanticplan.RatioAttributionNode:
			if ratio != nil {
				return nil, nil
			}
			copy := node
			ratio = &copy
		}
	}
	return additive, ratio
}

func sourceAggregateByID(plan *semanticplan.SemanticPlan, id string) (semanticplan.SourceAggregateNode, error) {
	for _, node := range plan.Nodes {
		if node.NodeBase().ID != id {
			continue
		}
		if source, ok := node.(semanticplan.SourceAggregateNode); ok {
			return source, nil
		}
		return semanticplan.SourceAggregateNode{}, fmt.Errorf("metric attribution input %q must be a source aggregate", id)
	}
	return semanticplan.SourceAggregateNode{}, fmt.Errorf("metric attribution input %q is missing", id)
}

func predicateSetContains(haystack, needles []semanticplan.Predicate) bool {
	if len(needles) > len(haystack) {
		return false
	}
	matched := make([]bool, len(haystack))
	for _, needle := range needles {
		found := false
		for i, candidate := range haystack {
			if matched[i] || !sameFilterIdentity(candidate.Filter, needle.Filter) {
				continue
			}
			matched[i] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func sameMetricAttributionRange(left, right semanticplan.MetricAttributionTimeRange) bool {
	return left.Start.Equal(right.Start) && left.End.Equal(right.End)
}

func sameFilterIdentity(left, right query.Filter) bool {
	return left.Field == right.Field && left.Operator == right.Operator && reflect.DeepEqual(left.Value, right.Value)
}
