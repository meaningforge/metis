package conversion

import (
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// BuildOutputSchema derives the stable Agent-facing result contract from the
// resolved projections. It never inspects rendered SQL or physical types.
func BuildOutputSchema(plan *semanticplan.SemanticPlan) (artifact.OutputSchema, error) {
	if plan == nil {
		return artifact.OutputSchema{}, outputSchemaError("semantic plan is required", nil)
	}
	if LoweringStrategyForPlan(plan) == SemanticLoweringAdditiveAttribution {
		return buildAdditiveAttributionOutputSchema(plan)
	}
	if LoweringStrategyForPlan(plan) == SemanticLoweringRatioAttribution {
		return buildRatioAttributionOutputSchema(plan)
	}
	if len(plan.Projections) == 0 {
		return artifact.OutputSchema{}, outputSchemaError("semantic plan has no output projections", nil)
	}

	columns := make([]artifact.OutputColumn, len(plan.Projections))
	for i, projection := range plan.Projections {
		if projection.Name == "" {
			return artifact.OutputSchema{}, outputSchemaError("output projection name is required", map[string]any{"projection_index": i})
		}
		column := artifact.OutputColumn{Name: projection.Name}
		grain := projection.Grain
		if grain == nil && projection.CustomCalendar != nil {
			grain = &projection.CustomCalendar.Grain
		}
		if grain != nil {
			grain := *grain
			column.Grain = &grain
		}
		switch projection.Kind {
		case semanticplan.ProjectionDimension:
			if projection.Field == nil {
				return artifact.OutputSchema{}, outputSchemaError("dimension output is missing its resolved field", map[string]any{"column": projection.Name})
			}
			column.Kind = artifact.OutputDimension
			column.Datatype = projection.Field.Datatype
		case semanticplan.ProjectionMetric:
			if projection.Metric == nil {
				return artifact.OutputSchema{}, outputSchemaError("metric output is missing its resolved metric", map[string]any{"column": projection.Name})
			}
			column.Kind = artifact.OutputMetric
			column.Datatype = projection.Metric.Datatype
		default:
			return artifact.OutputSchema{}, outputSchemaError("unsupported output projection kind", map[string]any{"column": projection.Name, "kind": projection.Kind})
		}
		columns[i] = column
	}
	return artifact.OutputSchema{Columns: columns}, nil
}

func buildRatioAttributionOutputSchema(plan *semanticplan.SemanticPlan) (artifact.OutputSchema, error) {
	attribution, numerator, denominator, err := ratioAttributionPhysicalInputs(plan)
	if err != nil {
		return artifact.OutputSchema{}, outputSchemaError("invalid ratio attribution output contract", map[string]any{"cause": err.Error()})
	}
	if numerator.Metric == nil || denominator.Metric == nil {
		return artifact.OutputSchema{}, outputSchemaError("ratio attribution output is missing governed operand evidence", map[string]any{"metric": attribution.MetricRef})
	}
	if len(numerator.Base.OutputGrain) != 1 || numerator.Base.OutputGrain[0].Field == nil {
		return artifact.OutputSchema{}, outputSchemaError("ratio attribution dimension output is missing its resolved field", map[string]any{"dimension": attribution.DimensionRef})
	}
	dimensionType := numerator.Base.OutputGrain[0].Field.Datatype
	numeratorType := numerator.Metric.Datatype
	denominatorType := denominator.Metric.Datatype
	decimal := ossie.DataTypeDecimal
	boolean := ossie.DataTypeBoolean
	return artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: attribution.DimensionRef, Kind: artifact.OutputDimension, Datatype: dimensionType},
		{Name: ratioAttributionBaselinePresentAlias, Kind: artifact.OutputDimension, Datatype: boolean},
		{Name: ratioAttributionCurrentPresentAlias, Kind: artifact.OutputDimension, Datatype: boolean},
		{Name: ratioAttributionBaselineNumeratorAlias, Kind: artifact.OutputMetric, Datatype: numeratorType},
		{Name: ratioAttributionBaselineDenominatorAlias, Kind: artifact.OutputMetric, Datatype: denominatorType},
		{Name: ratioAttributionCurrentNumeratorAlias, Kind: artifact.OutputMetric, Datatype: numeratorType},
		{Name: ratioAttributionCurrentDenominatorAlias, Kind: artifact.OutputMetric, Datatype: denominatorType},
		{Name: ratioAttributionBaselineRateAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionCurrentRateAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionBaselineWeightAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionCurrentWeightAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionBaselineDefinedAlias, Kind: artifact.OutputDimension, Datatype: boolean},
		{Name: ratioAttributionCurrentDefinedAlias, Kind: artifact.OutputDimension, Datatype: boolean},
		{Name: ratioAttributionSegmentDefinedAlias, Kind: artifact.OutputDimension, Datatype: boolean},
		{Name: ratioAttributionRateEffectAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionMixEffectAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionEntryEffectAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionExitEffectAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionSegmentEffectAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionBaselineRatioAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionCurrentRatioAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionRatioDeltaAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionDecomposedDeltaAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionResidualAlias, Kind: artifact.OutputMetric, Datatype: decimal},
		{Name: ratioAttributionDefinedAlias, Kind: artifact.OutputDimension, Datatype: boolean},
	}}, nil
}

func buildAdditiveAttributionOutputSchema(plan *semanticplan.SemanticPlan) (artifact.OutputSchema, error) {
	attribution, producer, err := additiveAttributionPhysicalInputs(plan)
	if err != nil {
		return artifact.OutputSchema{}, outputSchemaError("invalid additive attribution output contract", map[string]any{"cause": err.Error()})
	}
	if producer.Metric == nil {
		return artifact.OutputSchema{}, outputSchemaError("attribution metric output is missing governed metric evidence", map[string]any{"metric": attribution.MetricRef})
	}
	if len(producer.Base.OutputGrain) != 1 || producer.Base.OutputGrain[0].Field == nil {
		return artifact.OutputSchema{}, outputSchemaError("attribution dimension output is missing its resolved field", map[string]any{"dimension": attribution.DimensionRef})
	}
	metricType := producer.Metric.Datatype
	return artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: attribution.DimensionRef, Kind: artifact.OutputDimension, Datatype: producer.Base.OutputGrain[0].Field.Datatype},
		{Name: additiveAttributionBaselineValueAlias, Kind: artifact.OutputMetric, Datatype: metricType},
		{Name: additiveAttributionCurrentValueAlias, Kind: artifact.OutputMetric, Datatype: metricType},
		{Name: additiveAttributionDeltaAlias, Kind: artifact.OutputMetric, Datatype: metricType},
		{Name: additiveAttributionTotalDeltaAlias, Kind: artifact.OutputMetric, Datatype: metricType},
		{Name: additiveAttributionContributionAlias, Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	}}, nil
}

// Every caller reports a structure the planner built itself: a nil plan, a
// missing projection, or a projection kind the planner created. No query or
// model reaches these.
func outputSchemaError(message string, details map[string]any) error {
	return serrors.Internal(message, details)
}
