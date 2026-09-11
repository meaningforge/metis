package builder

import (
	"sort"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// SplitEvaluationPredicates assigns resolved predicates to source reads or to
// post-evaluation output. Cumulative metrics retain filters on their output
// time dimension so source construction does not change window semantics.
func SplitEvaluationPredicates(evaluationPlan *evaluation.MetricEvaluationPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) ([]semanticplan.Predicate, []semanticplan.PostEvaluationPredicate, error) {
	cumulativeTimeDimensions := map[string]struct{}{}
	if evaluationPlan != nil {
		for _, node := range evaluationPlan.Nodes {
			if node.Kind != evaluation.MetricEvaluationCumulative {
				continue
			}
			if node.Spec.Cumulative == nil {
				return nil, nil, &serrors.Error{
					Code:    serrors.ErrInternalInvariant,
					Message: "cumulative metric evaluation is missing its typed spec",
					Details: map[string]any{"metric": node.ID},
				}
			}
			cumulativeTimeDimensions[node.Spec.Cumulative.Spec.TimeDimension] = struct{}{}
		}
	}

	pre := make([]semanticplan.Predicate, 0, len(predicates))
	post := make([]semanticplan.PostEvaluationPredicate, 0)
	for _, predicate := range predicates {
		if predicate.Field == nil {
			post = append(post, semanticplan.PostEvaluationPredicate{Name: predicate.Filter.Field, Filter: predicate.Filter})
			continue
		}
		outputName := ""
		for _, group := range groups {
			if group.Field == nil || group.Field.Name != predicate.Field.Name || group.Dataset != predicate.Dataset {
				continue
			}
			if _, ok := cumulativeTimeDimensions[group.Field.Name]; ok {
				outputName = group.Name
				break
			}
			if _, ok := cumulativeTimeDimensions[unqualifiedName(group.Name)]; ok {
				outputName = group.Name
				break
			}
		}
		if outputName == "" {
			pre = append(pre, predicate)
			continue
		}
		post = append(post, semanticplan.PostEvaluationPredicate{Name: outputName, Filter: predicate.Filter})
	}
	return pre, post, nil
}

// ConstructionInput carries canonical typed evaluation nodes through source
// planning and enrichment before semantic-plan ownership is installed. It
// deliberately excludes metric dependency edges, which remain owned by
// evaluation.MetricEvaluationPlan.
type ConstructionInput struct {
	Requested                []string
	Nodes                    []semanticplan.SemanticPlanNode
	OutputGrain              []semanticplan.GroupBy
	PostEvaluationPredicates []semanticplan.PostEvaluationPredicate
	SourceRequirements       map[string]SourceRequirement
}

// RequiresComposed reports whether typed construction results require composed
// lowering rather than one compact source read.
func RequiresComposed(nodes []semanticplan.SemanticPlanNode, post []semanticplan.PostEvaluationPredicate) bool {
	if len(post) != 0 {
		return true
	}
	roots := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node == nil || node.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			return true
		}
		if _, ok, _ := ossie.MetricDefinitionFilter(semanticplan.NodeMetric(node)); ok {
			return true
		}
		source, ok := semanticplan.NodeSourceState(node)
		if !ok {
			return true
		}
		if source.Root.Name != "" {
			roots[source.Root.Name] = struct{}{}
		}
	}
	return len(roots) > 1
}

// EvaluationSeam records concrete node identity settled before source-aware
// enrichment. It prevents later construction steps from changing evaluation
// semantics by rewriting node kinds.
type EvaluationSeam map[string]semanticplan.SemanticPlanNodeKind

// CaptureEvaluationSeam snapshots typed evaluation identity.
func CaptureEvaluationSeam(nodes []semanticplan.SemanticPlanNode) EvaluationSeam {
	seam := make(EvaluationSeam, len(nodes))
	for _, node := range nodes {
		if node != nil {
			seam[node.NodeBase().ID] = node.Kind()
		}
	}
	return seam
}

// RequireEvaluationSeam fails closed when enrichment changes the concrete type
// of a node that construction had already settled.
func RequireEvaluationSeam(before EvaluationSeam, nodes []semanticplan.SemanticPlanNode) error {
	var moved []string
	for _, node := range nodes {
		if node == nil {
			continue
		}
		id := node.NodeBase().ID
		settled, ok := before[id]
		if ok && node.Kind() != settled {
			moved = append(moved, id+": "+string(settled)+" -> "+string(node.Kind()))
		}
	}
	if len(moved) == 0 {
		return nil
	}
	sort.Strings(moved)
	return &serrors.Error{
		Code:    serrors.ErrInternalInvariant,
		Message: "source planning changed evaluation semantics that metric evaluation construction had settled",
		Details: map[string]any{"nodes": strings.Join(moved, "; ")},
	}
}
