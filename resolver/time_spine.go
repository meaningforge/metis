package resolver

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// resolveTimeSpine activates model-level dense-calendar infrastructure only for
// time-relative metrics evaluated on their canonical time dimension. Ordinary
// metrics keep sparse fact-row semantics even when the model declares a spine.
func resolveTimeSpine(q *SemanticQuerySpec) error {
	if q == nil || q.Model == nil || q.Model.Model == nil {
		return nil
	}
	needsSpine := false
	var canonical string
	for _, metric := range q.EvaluationMetrics {
		if metric.Metric == nil {
			continue
		}
		_, cumulative, cErr := ossie.CumulativeSpec(metric.Metric)
		if cErr != nil {
			return invalidQuery("invalid cumulative metric extension", map[string]any{"metric": metric.Name, "cause": cErr.Error()})
		}
		_, offset, oErr := ossie.TimeOffsetSpec(metric.Metric)
		if oErr != nil {
			return invalidQuery("invalid time-offset metric extension", map[string]any{"metric": metric.Name, "cause": oErr.Error()})
		}
		offsetToGrainSpec, offsetToGrain, gErr := ossie.OffsetToGrainSpec(metric.Metric)
		if gErr != nil {
			return invalidQuery("invalid offset-to-grain metric extension", map[string]any{"metric": metric.Name, "cause": gErr.Error()})
		}
		if !cumulative && !offset && !offsetToGrain {
			continue
		}
		needsSpine = true
		if metric.TimeBinding != nil {
			canonical = metric.TimeBinding.TimeDimension
		} else if offsetToGrain {
			canonical = offsetToGrainSpec.TimeDimension
		}
	}
	if !needsSpine {
		return nil
	}

	var requested *ResolvedDimension
	for i := range q.Dimensions {
		d := &q.Dimensions[i]
		if d.Grain == nil || !isTimeDimension(d.Field) {
			continue
		}
		if canonical != "" && unqualifiedDimensionName(d.Name) != canonical {
			continue
		}
		requested = d
		break
	}
	if requested == nil {
		return nil
	}

	spec, ok, err := ossie.TimeSpine(q.Model.Model)
	if err != nil {
		return invalidQuery("invalid model time spine", map[string]any{"cause": err.Error()})
	}
	if !ok {
		return nil
	}
	if !timeSpineSupportsGrain(spec, *requested.Grain) {
		return invalidQuery("requested time grain is not supported by the model time spine", map[string]any{"time_dimension": requested.Name, "grain": *requested.Grain, "supported_grains": spec.Grains})
	}
	dataset := q.Model.Datasets[spec.Dataset]
	if dataset == nil {
		return invalidQuery("time-spine dataset is not present in the canonical manifest", map[string]any{"dataset": spec.Dataset})
	}
	var field *ossie.Field
	for i := range dataset.Fields {
		if dataset.Fields[i].Name == spec.TimeDimension {
			field = &dataset.Fields[i]
			break
		}
	}
	if field == nil {
		return invalidQuery("time-spine time dimension is not present in the canonical manifest", map[string]any{"dataset": spec.Dataset, "time_dimension": spec.TimeDimension})
	}
	q.TimeSpine = &ResolvedTimeSpine{Spec: spec, Dataset: dataset, TimeField: field, QueryTimeDimension: unqualifiedDimensionName(requested.Name), RequestedGrain: *requested.Grain}
	return nil
}

func timeSpineSupportsGrain(spec ossie.TimeSpineSpec, grain query.TimeGrain) bool {
	want := string(grain)
	for _, candidate := range spec.Grains {
		if candidate == want {
			return true
		}
	}
	return false
}
