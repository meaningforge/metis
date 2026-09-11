package semantic

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

// appendMetricConstraintEvidence preserves resolved metric-definition evidence
// on the canonical SemanticPlan. Node placement and lineage remain graph-owned;
// these steps describe declarative metric constraints carried by projected
// metrics, which every plan has whether or not it composes nodes.
func appendMetricConstraintEvidence(explanation *QueryExplanation, plan *semanticplan.SemanticPlan) error {
	for _, projection := range plan.Projections {
		if projection.Kind != semanticplan.ProjectionMetric || projection.Metric == nil {
			continue
		}
		metric := projection.Metric
		if spec, ok, err := ossie.FillSpec(metric); err != nil {
			return err
		} else if ok {
			explanation.Steps = append(explanation.Steps, ExplanationStep{
				Kind:    ExplanationFill,
				Subject: projection.Name,
				Details: map[string]any{"policy": spec.Policy},
			})
		}
		if spec, ok, err := ossie.MetricDefinitionFilter(metric); err != nil {
			return err
		} else if ok {
			explanation.Steps = append(explanation.Steps, ExplanationStep{
				Kind:    ExplanationDefinitionFilter,
				Subject: projection.Name,
				Details: map[string]any{
					"stage":   ossie.EffectiveMetricDefinitionFilterStage(spec),
					"filters": spec.Filters,
				},
			})
		}
	}
	return nil
}
