package policy

import (
	"reflect"
	"sort"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/planner/builder"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/resolver"
)

// ResolvedWork owns no semantic authority: Query and Evaluation are the exact
// results prepared by the operation's Resolver and metric-evaluation boundary.
// Multi-query operations collect their union before evaluating a policy once.
type ResolvedWork struct {
	Query      *resolver.SemanticQuerySpec
	Evaluation *evaluation.MetricEvaluationPlan
}

// CollectWorkload builds canonical bounded policy input without constructing
// a SemanticPlan. Models must be from one immutable generation and all queries
// must retain identical selected expression evidence within each model.
func CollectWorkload(principal auth.Principal, projectID string, queries []ResolvedWork) (Request, error) {
	if len(queries) == 0 || len(queries) > MaxSources {
		return Request{}, ErrInvalid
	}
	req := Request{Principal: principal, ProjectID: projectID}
	models := map[string]*resolver.SemanticQuerySpec{}
	sources := map[DatasetRef]map[FieldRef]bool{}
	fieldCount := 0
	for _, work := range queries {
		q := work.Query
		if q == nil || q.Project != projectID || q.Model == nil || q.Model.Model == nil {
			return Request{}, ErrInvalid
		}
		model := q.Model.Model.Name
		if previous := models[model]; previous != nil && (previous.Model != q.Model || !reflect.DeepEqual(previous.FieldExpressions, q.FieldExpressions)) {
			return Request{}, ErrInvalid
		}
		models[model] = q
		dependencies, err := builder.RequiredDataDependencies(q, work.Evaluation)
		if err != nil {
			return Request{}, ErrInvalid
		}
		for _, dataset := range dependencies.Datasets {
			ref := DatasetRef{Model: model, Dataset: dataset}
			if sources[ref] == nil {
				sources[ref] = map[FieldRef]bool{}
			}
		}
		seeds := make([]FieldRef, 0, len(dependencies.Fields))
		for _, field := range dependencies.Fields {
			seeds = append(seeds, FieldRef{Dataset: DatasetRef{Model: model, Dataset: field.Dataset}, Field: field.Field})
		}
		closure, err := FieldClosure(q, seeds)
		if err != nil {
			return Request{}, ErrInvalid
		}
		for _, field := range closure {
			if sources[field.Dataset] == nil {
				sources[field.Dataset] = map[FieldRef]bool{}
			}
			if !sources[field.Dataset][field] {
				fieldCount++
				sources[field.Dataset][field] = true
			}
		}
		if len(sources) > MaxSources || fieldCount > MaxFields {
			return Request{}, ErrInvalid
		}
	}
	for dataset, fields := range sources {
		source := Source{Dataset: dataset}
		for field := range fields {
			source.RequiredFields = append(source.RequiredFields, field)
		}
		sort.Slice(source.RequiredFields, func(i, j int) bool { return source.RequiredFields[i].Field < source.RequiredFields[j].Field })
		req.Sources = append(req.Sources, source)
	}
	sort.Slice(req.Sources, func(i, j int) bool {
		if req.Sources[i].Dataset.Model != req.Sources[j].Dataset.Model {
			return req.Sources[i].Dataset.Model < req.Sources[j].Dataset.Model
		}
		return req.Sources[i].Dataset.Dataset < req.Sources[j].Dataset.Dataset
	})
	if !validRequest(req) {
		return Request{}, ErrInvalid
	}
	return cloneRequest(req), nil
}
