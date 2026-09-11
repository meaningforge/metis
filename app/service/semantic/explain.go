package semantic

import (
	"context"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

type ExplanationStepKind string

const (
	ExplanationSource                ExplanationStepKind = "source"
	ExplanationRelationship          ExplanationStepKind = "relationship"
	ExplanationDefinitionFilter      ExplanationStepKind = "definition_filter"
	ExplanationAggregation           ExplanationStepKind = "aggregation"
	ExplanationTimeAlignment         ExplanationStepKind = "time_alignment"
	ExplanationTimeOffset            ExplanationStepKind = "time_offset"
	ExplanationFill                  ExplanationStepKind = "fill"
	ExplanationSemiAdditiveSelection ExplanationStepKind = "semi_additive_selection"
	ExplanationComposition           ExplanationStepKind = "composition"
	ExplanationQueryFilter           ExplanationStepKind = "query_filter"
	ExplanationGrouping              ExplanationStepKind = "grouping"
	ExplanationOrdering              ExplanationStepKind = "ordering"
	ExplanationLimit                 ExplanationStepKind = "limit"
)

type QueryExplanation struct {
	DataConstraintsApplied bool                                 `json:"data_constraints_applied,omitempty"`
	Project                string                               `json:"project"`
	Model                  string                               `json:"model"`
	Metrics                []string                             `json:"metrics,omitempty"`
	Dimensions             []string                             `json:"dimensions,omitempty"`
	Relationships          []RelationshipEvidence               `json:"relationship_paths,omitempty"`
	Steps                  []ExplanationStep                    `json:"steps"`
	SemanticPlan           semanticplan.SemanticPlanExplanation `json:"semantic_plan"`
	OutputSchema           artifact.OutputSchema                `json:"output_schema"`
}

type RelationshipEvidence struct {
	Name        string `json:"name"`
	FromDataset string `json:"from_dataset"`
	ToDataset   string `json:"to_dataset"`
}

type ExplanationStep struct {
	Kind       ExplanationStepKind `json:"kind"`
	Subject    string              `json:"subject"`
	Datasets   []string            `json:"datasets,omitempty"`
	DependsOn  []string            `json:"depends_on,omitempty"`
	Dimensions []string            `json:"dimensions,omitempty"`
	Details    map[string]any      `json:"details,omitempty"`
}

// Explain runs the same Renderer selection, semantic resolution, validation, and
// planning path as Compile. Agent-facing semantic evidence is derived from the
// canonical SemanticPlan; node lineage and placement evidence come from its
// node DAG rather than transitional metric-evaluation nodes.
func (s *CompileService) Explain(ctx context.Context, req CompileRequest) (*QueryExplanation, error) {
	plan, _, err := s.prepareSemanticQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	return buildQueryExplanation(plan)
}

func buildQueryExplanation(plan *semanticplan.SemanticPlan) (*QueryExplanation, error) {
	if plan == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan is required"}
	}
	outputSchema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		return nil, err
	}
	semanticExplanation, err := semanticplan.Explain(plan)
	if err != nil {
		return nil, err
	}
	explanation := &QueryExplanation{
		DataConstraintsApplied: plan.PolicyScope != "",
		Project:                plan.Model.Project,
		Model:                  plan.Model.Name,
		SemanticPlan:           semanticExplanation,
		OutputSchema:           outputSchema,
	}

	for _, projection := range plan.Projections {
		switch projection.Kind {
		case semanticplan.ProjectionMetric:
			explanation.Metrics = append(explanation.Metrics, projection.Name)
		case semanticplan.ProjectionDimension:
			explanation.Dimensions = append(explanation.Dimensions, projection.Name)
		}
	}
	appendNodeDAGEvidence(explanation, plan.Nodes, plan.Output.Grain, semanticExplanation)
	if len(plan.Nodes) == 0 {
		appendFlatPlanEvidence(explanation, plan)
	}
	if err := appendMetricConstraintEvidence(explanation, plan); err != nil {
		return nil, err
	}
	for _, sortSpec := range plan.Sorts {
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:     ExplanationOrdering,
			Subject:  sortSpec.Name,
			Datasets: nonEmptyStrings(sortSpec.Dataset),
			Details:  map[string]any{"direction": sortSpec.Direction},
		})
	}
	if plan.Limit != nil {
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind: ExplanationLimit, Subject: "query", Details: map[string]any{"limit": *plan.Limit},
		})
	}
	return explanation, nil
}

func appendFlatPlanEvidence(explanation *QueryExplanation, plan *semanticplan.SemanticPlan) {
	for _, projection := range plan.Projections {
		if projection.Kind != semanticplan.ProjectionMetric {
			continue
		}
		datasets := append([]string(nil), projection.Datasets...)
		if projection.Dataset != "" {
			datasets = append(datasets, projection.Dataset)
		}
		if len(datasets) == 0 && plan.Root.Name != "" {
			datasets = append(datasets, plan.Root.Name)
		}
		explanation.Steps = append(explanation.Steps,
			ExplanationStep{Kind: ExplanationSource, Subject: projection.Name, Datasets: datasets},
			ExplanationStep{Kind: ExplanationAggregation, Subject: projection.Name, Datasets: datasets},
		)
	}
	for _, join := range plan.Joins {
		if join.Relationship == nil {
			continue
		}
		explanation.Relationships = append(explanation.Relationships, RelationshipEvidence{
			Name:        join.Relationship.Name,
			FromDataset: join.FromDataset,
			ToDataset:   join.ToDataset,
		})
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:     ExplanationRelationship,
			Subject:  join.Relationship.Name,
			Datasets: []string{join.FromDataset, join.ToDataset},
		})
	}
	for _, predicate := range plan.Predicates {
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:     ExplanationQueryFilter,
			Subject:  predicate.Filter.Field,
			Datasets: nonEmptyStrings(predicate.Dataset),
		})
	}
	for _, group := range plan.Groups {
		details := map[string]any{}
		if group.Grain != nil {
			details["grain"] = *group.Grain
		}
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:     ExplanationGrouping,
			Subject:  group.Name,
			Datasets: nonEmptyStrings(group.Dataset),
			Details:  details,
		})
	}
}

func appendNodeDAGEvidence(explanation *QueryExplanation, nodes []semanticplan.SemanticPlanNode, outputGrain []semanticplan.GroupBy, semanticExplanation semanticplan.SemanticPlanExplanation) {
	seenRelationships := map[string]struct{}{}
	for _, node := range nodes {
		var joins []semanticplan.Join
		switch node := node.(type) {
		case semanticplan.SourceAggregateNode:
			joins = node.Source.Joins
		case semanticplan.PostAggregateNode:
			joins = node.Source.Joins
		case semanticplan.JoinAggregatesNode:
			joins = node.Source.Joins
		case semanticplan.CrossJoinAggregatesNode:
			joins = node.Source.Joins
		case semanticplan.CumulativeWindowNode:
			joins = node.Source.Joins
		case semanticplan.TimeOffsetNode:
			joins = node.Source.Joins
		case semanticplan.OffsetToGrainNode:
			joins = node.Source.Joins
		case semanticplan.ConversionNode:
			joins = node.Source.Joins
		case semanticplan.SemiAdditiveNode:
			joins = node.Source.Joins
		case semanticplan.SourceSelectionNode:
			joins = node.Source.Joins
		}
		for _, join := range joins {
			if join.Relationship == nil {
				continue
			}
			key := join.Relationship.Name + "\x00" + join.FromDataset + "\x00" + join.ToDataset
			if _, ok := seenRelationships[key]; ok {
				continue
			}
			seenRelationships[key] = struct{}{}
			explanation.Relationships = append(explanation.Relationships, RelationshipEvidence{
				Name:        join.Relationship.Name,
				FromDataset: join.FromDataset,
				ToDataset:   join.ToDataset,
			})
			explanation.Steps = append(explanation.Steps, ExplanationStep{
				Kind:     ExplanationRelationship,
				Subject:  join.Relationship.Name,
				Datasets: []string{join.FromDataset, join.ToDataset},
			})
		}
	}

	for _, node := range semanticExplanation.Nodes {
		base := ExplanationStep{
			Subject:    node.ID,
			Datasets:   append([]string(nil), node.SourceDatasets...),
			DependsOn:  append([]string(nil), node.Inputs...),
			Dimensions: append([]string(nil), node.Dimensions...),
			Details: map[string]any{
				"boundary":           node.Boundary,
				"relationship_paths": node.RelationshipPaths,
			},
		}
		if len(node.PopulationPreservation) != 0 {
			base.Details["population_preservation"] = node.PopulationPreservation
		}
		if node.FanoutSafe != nil {
			base.Details["fanout_safe"] = *node.FanoutSafe
		}
		switch node.Kind {
		case semanticplan.SemanticPlanNodeSourceAggregate:
			step := base
			step.Kind = ExplanationSource
			explanation.Steps = append(explanation.Steps, step)
			step = base
			step.Kind = ExplanationAggregation
			explanation.Steps = append(explanation.Steps, step)
		case semanticplan.SemanticPlanNodePostAggregate, semanticplan.SemanticPlanNodeJoinAggregates, semanticplan.SemanticPlanNodeCrossJoinAggregates, semanticplan.SemanticPlanNodeConversion:
			step := base
			step.Kind = ExplanationComposition
			explanation.Steps = append(explanation.Steps, step)
		case semanticplan.SemanticPlanNodeCumulativeWindow:
			step := base
			step.Kind = ExplanationAggregation
			explanation.Steps = append(explanation.Steps, step)
		case semanticplan.SemanticPlanNodeTimeOffset:
			step := base
			step.Kind = ExplanationTimeAlignment
			explanation.Steps = append(explanation.Steps, step)
			step = base
			step.Kind = ExplanationTimeOffset
			explanation.Steps = append(explanation.Steps, step)
		case semanticplan.SemanticPlanNodeOffsetToGrain:
			step := base
			step.Kind = ExplanationTimeAlignment
			explanation.Steps = append(explanation.Steps, step)
		case semanticplan.SemanticPlanNodeSemiAdditiveLast, semanticplan.SemanticPlanNodeSemiAdditiveFirst:
			step := base
			step.Kind = ExplanationSemiAdditiveSelection
			explanation.Steps = append(explanation.Steps, step)
		}
		for _, predicate := range node.Predicates {
			explanation.Steps = append(explanation.Steps, ExplanationStep{
				Kind:    ExplanationQueryFilter,
				Subject: predicate.Subject,
				Details: map[string]any{"scope": predicate.Scope, "owner_node_id": predicate.OwnerNodeID, "proof": predicate.Proof},
			})
		}
	}
	for _, predicate := range semanticExplanation.Predicates {
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:    ExplanationQueryFilter,
			Subject: predicate.Subject,
			Details: map[string]any{"scope": predicate.Scope, "proof": predicate.Proof},
		})
	}
	for _, group := range outputGrain {
		details := map[string]any{}
		if group.Grain != nil {
			details["grain"] = *group.Grain
		}
		explanation.Steps = append(explanation.Steps, ExplanationStep{
			Kind:     ExplanationGrouping,
			Subject:  group.Name,
			Datasets: nonEmptyStrings(group.Dataset),
			Details:  details,
		})
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
