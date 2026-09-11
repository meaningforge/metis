package resolver

import (
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

type ResolvedCustomCalendarLevel struct {
	Grain                     ossie.CustomCalendarGrain
	BucketField               *ossie.Field
	OrdinalField              *ossie.Field
	BucketSelectedExpression  string
	OrdinalSelectedExpression string
}

type ResolvedCustomCalendarGrain struct {
	Spec                      ossie.CustomCalendarSpec
	Grain                     ossie.CustomCalendarGrain
	Dataset                   *ossie.Dataset
	BaseTimeField             *ossie.Field
	BucketField               *ossie.Field
	OrdinalField              *ossie.Field
	BucketSelectedExpression  string
	OrdinalSelectedExpression string
	Levels                    map[string]*ResolvedCustomCalendarLevel
}

func resolveCustomCalendarGrain(model *manifest.ModelIndex, dimension *manifest.FieldHandle, grain query.TimeGrain) (*ResolvedCustomCalendarGrain, error) {
	if validTimeGrain(grain) {
		return nil, nil
	}
	if model == nil || model.Model == nil {
		return nil, invalidQuery("unsupported time grain", map[string]any{"grain": grain})
	}
	spec, ok, err := ossie.CustomCalendar(model.Model)
	if err != nil {
		return nil, invalidQuery("invalid custom calendar", map[string]any{"grain": grain, "cause": err.Error()})
	}
	if !ok {
		return nil, invalidQuery("unsupported time grain", map[string]any{"grain": grain})
	}
	customGrain, ok := ossie.CustomCalendarGrainByName(spec, string(grain))
	if !ok {
		return nil, invalidQuery("unsupported time grain", map[string]any{"grain": grain})
	}
	if dimension == nil || dimension.Field == nil || dimension.Dataset != spec.Dataset || dimension.Field.Name != spec.BaseTime {
		return nil, invalidQuery("custom calendar grain can only be applied to its canonical base time dimension", map[string]any{"grain": grain, "calendar_dataset": spec.Dataset, "base_time_dimension": spec.BaseTime})
	}
	dataset := model.Datasets[spec.Dataset]
	if dataset == nil {
		return nil, invalidQuery("custom calendar dataset is not indexed", map[string]any{"dataset": spec.Dataset})
	}
	base := model.Fields[spec.Dataset+"."+spec.BaseTime]
	if base == nil {
		return nil, invalidQuery("custom calendar base time field is not indexed", map[string]any{"grain": grain, "field": spec.BaseTime})
	}
	levels := make(map[string]*ResolvedCustomCalendarLevel, len(spec.Grains))
	for _, levelGrain := range spec.Grains {
		bucket := model.Fields[spec.Dataset+"."+levelGrain.BucketDimension]
		ordinal := model.Fields[spec.Dataset+"."+levelGrain.OrdinalDimension]
		if bucket == nil || ordinal == nil {
			return nil, invalidQuery("custom calendar fields are not indexed", map[string]any{"grain": levelGrain.Name})
		}
		levels[levelGrain.Name] = &ResolvedCustomCalendarLevel{Grain: levelGrain, BucketField: bucket.Field, OrdinalField: ordinal.Field}
	}
	current := levels[customGrain.Name]
	if current == nil {
		return nil, invalidQuery("custom calendar query grain is not indexed", map[string]any{"grain": grain})
	}
	return &ResolvedCustomCalendarGrain{Spec: spec, Grain: customGrain, Dataset: dataset, BaseTimeField: base.Field, BucketField: current.BucketField, OrdinalField: current.OrdinalField, Levels: levels}, nil
}
