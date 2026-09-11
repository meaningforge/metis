package attribution

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// MaxMetricAttributionDimensions bounds the number of independent
// decompositions one resolved request may ask Metis to compile. Each dimension
// produces its own SemanticPlan and physical query, so this is deliberately a
// request-work bound rather than a SQL grouping bound.
const MaxMetricAttributionDimensions = 16

// ResolvedMetricAttributionRequest is the internal, already-resolved request
// consumed by attribution planning. Dimensions are independent decomposition
// requests; they never describe one combined multi-column attribution grain.
type ResolvedMetricAttributionRequest struct {
	ProjectID        string
	MetricRef        string
	TimeDimensionRef string
	Baseline         semanticplan.MetricAttributionTimeRange
	Current          semanticplan.MetricAttributionTimeRange
	Dimensions       []string
	Filters          []semanticplan.Predicate
}

// ValidateResolvedMetricAttributionRequest validates semantic shape only. It
// does not perform manifest lookup, natural-language interpretation, or SQL
// lowering.
func ValidateResolvedMetricAttributionRequest(request ResolvedMetricAttributionRequest) error {
	if strings.TrimSpace(request.ProjectID) == "" {
		return fmt.Errorf("metric attribution request requires a project")
	}
	if strings.TrimSpace(request.MetricRef) == "" {
		return fmt.Errorf("metric attribution request requires a metric")
	}
	if strings.TrimSpace(request.TimeDimensionRef) == "" {
		return fmt.Errorf("metric attribution request requires a time dimension")
	}
	if err := validateMetricAttributionTimeRange("baseline", request.Baseline); err != nil {
		return err
	}
	if err := validateMetricAttributionTimeRange("current", request.Current); err != nil {
		return err
	}
	if len(request.Dimensions) == 0 {
		return fmt.Errorf("metric attribution request requires at least one resolved dimension")
	}
	if len(request.Dimensions) > MaxMetricAttributionDimensions {
		return fmt.Errorf("metric attribution request exceeds the maximum of %d resolved dimensions", MaxMetricAttributionDimensions)
	}
	seenDimensions := make(map[string]struct{}, len(request.Dimensions))
	for _, dimension := range request.Dimensions {
		if strings.TrimSpace(dimension) == "" {
			return fmt.Errorf("metric attribution request contains an empty resolved dimension")
		}
		if dimension == request.TimeDimensionRef {
			return fmt.Errorf("metric attribution decomposition dimension must differ from its time dimension")
		}
		if _, exists := seenDimensions[dimension]; exists {
			return fmt.Errorf("metric attribution request contains duplicate resolved dimension %q", dimension)
		}
		seenDimensions[dimension] = struct{}{}
	}
	for _, filter := range request.Filters {
		if strings.TrimSpace(filter.Filter.Field) == "" || strings.TrimSpace(filter.Dataset) == "" || filter.Field == nil || !filter.Expression.IsResolved() {
			return fmt.Errorf("metric attribution shared filter requires a resolved field")
		}
		if filter.Filter.Field == request.TimeDimensionRef {
			return fmt.Errorf("metric attribution time range must be expressed by baseline/current periods, not a shared time filter")
		}
	}
	return nil
}

// canonicalMetricAttributionRequest returns an owned request whose independent
// dimensions have deterministic canonical-ref ordering. Filter order is kept:
// filters are shared semantics, not bundle query ordering.
func canonicalMetricAttributionRequest(request ResolvedMetricAttributionRequest) ResolvedMetricAttributionRequest {
	canonical := request
	canonical.Dimensions = append([]string(nil), request.Dimensions...)
	sort.Strings(canonical.Dimensions)
	canonical.Filters = append([]semanticplan.Predicate(nil), request.Filters...)
	return canonical
}

func validateMetricAttributionTimeRange(name string, period semanticplan.MetricAttributionTimeRange) error {
	return semanticplan.ValidateMetricAttributionTimeRange(name, period)
}

// BuildAdditiveAttributionNode projects a proven additive MetricAttributionPlan
// into a typed SemanticPlan node. It never re-derives metric algebra. The
// resolved time dimension is carried as typed physical evidence so later
// lowering can add period predicates without consulting the manifest again.
func BuildAdditiveAttributionNode(
	nodeID string,
	request ResolvedMetricAttributionRequest,
	attribution MetricAttributionPlan,
	input semanticplan.SemanticPlanNodeInput,
	output semanticplan.GroupBy,
	timeDimension semanticplan.GroupBy,
) (semanticplan.AdditiveAttributionNode, error) {
	if err := ValidateResolvedMetricAttributionRequest(request); err != nil {
		return semanticplan.AdditiveAttributionNode{}, err
	}
	if err := ValidateMetricAttributionPlan(attribution); err != nil {
		return semanticplan.AdditiveAttributionNode{}, err
	}
	if strings.TrimSpace(nodeID) == "" {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution node requires an id")
	}
	if attribution.Metric != request.MetricRef || attribution.Exactness != MetricAttributionExact || attribution.Strategy != semanticplan.MetricAttributionAdditiveContribution {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution node requires an exact additive attribution plan for metric %q", request.MetricRef)
	}
	if len(attribution.Components) != 1 || attribution.Components[0].Metric != request.MetricRef || attribution.Components[0].Role != MetricAttributionValue {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution plan does not identify metric %q as its value component", request.MetricRef)
	}
	if attribution.Reconciliation != semanticplan.MetricAttributionReconcileSegmentDelta {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution plan has incompatible reconciliation %q", attribution.Reconciliation)
	}
	if strings.TrimSpace(input.NodeID) == "" {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution node requires one semantic input")
	}
	dimension := output.Name
	if !containsMetricAttributionDimension(request.Dimensions, dimension) {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution output dimension %q is not present in the resolved request", dimension)
	}
	if !semanticplan.SameGrain(input.Grain, []semanticplan.GroupBy{output}) {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution input grain must match its decomposition grain")
	}
	if timeDimension.Name != request.TimeDimensionRef {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution time dimension %q does not match resolved time dimension %q", timeDimension.Name, request.TimeDimensionRef)
	}
	if strings.TrimSpace(timeDimension.Dataset) == "" || !timeDimension.Expression.IsResolved() {
		return semanticplan.AdditiveAttributionNode{}, fmt.Errorf("additive attribution time dimension %q requires resolved dataset and expression evidence", request.TimeDimensionRef)
	}

	node := semanticplan.AdditiveAttributionNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        nodeID,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySemanticIsolation),
			Inputs:                    []semanticplan.SemanticPlanNodeInput{{NodeID: input.NodeID, Grain: append([]semanticplan.GroupBy(nil), input.Grain...)}},
			Dimensions:                []string{dimension},
			OutputGrain:               []semanticplan.GroupBy{output},
		},
		MetricState:      semanticplan.SemanticMetricState{Metrics: []string{request.MetricRef}},
		MetricRef:        request.MetricRef,
		TimeDimensionRef: request.TimeDimensionRef,
		TimeDimension:    timeDimension,
		DimensionRef:     dimension,
		Baseline:         request.Baseline,
		Current:          request.Current,
	}
	if err := semanticplan.ValidateNode(node); err != nil {
		return semanticplan.AdditiveAttributionNode{}, err
	}
	return node, nil
}

func containsMetricAttributionDimension(dimensions []string, candidate string) bool {
	for _, dimension := range dimensions {
		if dimension == candidate {
			return true
		}
	}
	return false
}

func additiveAttributionDecompositionKind(semanticplan.AdditiveAttributionNode) string {
	return "additive"
}

func additiveAttributionStrategy(semanticplan.AdditiveAttributionNode) semanticplan.MetricAttributionStrategy {
	return semanticplan.MetricAttributionAdditiveContribution
}

func additiveAttributionReconciliation(semanticplan.AdditiveAttributionNode) semanticplan.MetricAttributionReconciliation {
	return semanticplan.MetricAttributionReconcileSegmentDelta
}

func canonicalMetricAttributionInstant(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
