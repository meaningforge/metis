package resolver

import (
	"context"
	"reflect"

	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

type Resolver struct {
	manifest              *manifest.Store
	extensionInventory    *extension.Inventory
	extensionCapabilities *extension.Registry
}

func New(store *manifest.Store) *Resolver {
	return &Resolver{
		manifest:              store,
		extensionInventory:    extension.NewInventory(),
		extensionCapabilities: extension.NewRegistry(),
	}
}

// WithExtensionCapabilities gives Renderer-aware resolution the same explicit
// interpreter inventory and capability registry used by the service boundary.
func (r *Resolver) WithExtensionCapabilities(inventory *extension.Inventory, capabilities *extension.Registry) *Resolver {
	if r == nil {
		return r
	}
	if inventory != nil {
		r.extensionInventory = inventory
	}
	if capabilities != nil {
		r.extensionCapabilities = capabilities
	}
	return r
}

func (r *Resolver) Resolve(ctx context.Context, q query.SemanticQuery) (*SemanticQuerySpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Project == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	if q.Model == "" {
		return nil, invalidQuery("model is required", nil)
	}
	if len(q.Metrics) == 0 && len(q.Dimensions) == 0 {
		return nil, invalidQuery("at least one metric or dimension is required", nil)
	}
	if q.Limit != nil && *q.Limit <= 0 {
		return nil, invalidQuery("limit must be greater than zero", map[string]any{"limit": *q.Limit})
	}
	if err := q.Filters.Validate(); err != nil {
		return nil, invalidQuery("invalid filters", map[string]any{"cause": err.Error()})
	}
	if r == nil || r.manifest == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "semantic manifest is not loaded"}
	}
	lookup, err := r.manifest.Lookup()
	if err != nil {
		return nil, err
	}
	if lookup == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "semantic manifest is not loaded"}
	}
	modelLookup, err := lookup.Model(q.Project, q.Model)
	if err != nil {
		return nil, err
	}
	model := modelLookup.ManifestModel()
	out := &SemanticQuerySpec{Project: q.Project, Model: model, Intent: q.Intent, Limit: q.Limit}
	neededDatasets := map[string]struct{}{}
	for _, ref := range q.Metrics {
		metric, err := model.Metric(ref.Name)
		if err != nil {
			return nil, err
		}
		datasets := metricDatasets(metric, model)
		if len(datasets) == 0 {
			return nil, invalidQuery("metric dataset cannot be determined from the canonical manifest dependency index; qualify semantic references when scope is ambiguous", map[string]any{"metric": ref.Name})
		}
		for _, ds := range datasets {
			neededDatasets[ds] = struct{}{}
		}
		resolvedMetric := ResolvedMetric{Name: ref.Name, Metric: metric, Datasets: datasets}
		if binding, ok, err := ossie.MetricTimeBinding(metric); err != nil {
			return nil, invalidQuery("invalid metric time binding", map[string]any{"metric": ref.Name, "cause": err.Error()})
		} else if ok {
			resolvedMetric.TimeBinding = &binding
		}
		out.Metrics = append(out.Metrics, resolvedMetric)
	}
	for _, ref := range q.Dimensions {
		h, err := model.Dimension(ref.Name)
		if err != nil {
			return nil, err
		}
		if ref.Grain != nil && !isTimeDimension(h.Field) {
			return nil, invalidQuery("time grain can only be applied to a time dimension", map[string]any{"dimension": ref.Name})
		}
		var customCalendar *ResolvedCustomCalendarGrain
		if ref.Grain != nil {
			customCalendar, err = resolveCustomCalendarGrain(model, h, *ref.Grain)
			if err != nil {
				details := map[string]any{"dimension": ref.Name, "grain": *ref.Grain}
				if typedErr, ok := err.(*serrors.Error); ok && typedErr.Details != nil {
					for k, v := range typedErr.Details {
						details[k] = v
					}
				}
				return nil, invalidQuery(err.Error(), details)
			}
		}
		neededDatasets[h.Dataset] = struct{}{}
		out.Dimensions = append(out.Dimensions, ResolvedDimension{Name: ref.Name, Grain: ref.Grain, Dataset: h.Dataset, Field: h.Field, CustomCalendar: customCalendar})
	}
	hasMetricFilter := false
	var existenceFilters []query.Filter
	for _, f := range q.Filters.RootConjuncts() {
		if f.Kind == query.PredicateExists {
			existenceFilters = append(existenceFilters, f)
			continue
		}
		if f.Kind != "" {
			if predicateContainsKind(f, query.PredicateExists) {
				return nil, unsupportedQueryShape("relationship existence must be a root AND conjunct", nil)
			}
			resolved, dataset, err := resolveBooleanPredicate(model, f)
			if err != nil {
				return nil, err
			}
			out.BooleanFilters = append(out.BooleanFilters, resolved)
			neededDatasets[dataset] = struct{}{}
			continue
		}
		if err := validateFilter(f); err != nil {
			return nil, err
		}
		resolved, err := resolveFilter(model, f)
		if err != nil {
			return nil, err
		}
		out.Filters = append(out.Filters, resolved)
		if resolved.Kind == FilterTargetMetric {
			hasMetricFilter = true
		} else {
			neededDatasets[resolved.Dataset] = struct{}{}
		}
	}
	for _, order := range q.OrderBy {
		resolvedOrder, datasets, err := resolveOrderBy(model, order)
		if err != nil {
			return nil, err
		}
		for _, ds := range datasets {
			neededDatasets[ds] = struct{}{}
		}
		out.OrderBy = append(out.OrderBy, resolvedOrder)
	}
	evaluationNames := make([]string, 0, len(out.Metrics)+len(out.OrderBy)+len(out.Filters))
	for _, metric := range out.Metrics {
		evaluationNames = append(evaluationNames, metric.Name)
	}
	for _, order := range out.OrderBy {
		if order.Kind == OrderTargetMetric {
			evaluationNames = append(evaluationNames, order.Name)
		}
	}
	for _, filter := range out.Filters {
		if filter.Kind == FilterTargetMetric {
			evaluationNames = append(evaluationNames, filter.Filter.Field)
		}
	}
	if len(evaluationNames) == 0 {
		root := out.Dimensions[0].Dataset
		out.RootDataset = root
		if err := resolveRelationshipExistence(model, out, existenceFilters, root); err != nil {
			return nil, err
		}
		required := make([]string, 0, len(neededDatasets))
		for ds := range neededDatasets {
			required = append(required, ds)
		}
		pathPlan, err := model.ResolveDatasetPaths(root, required)
		if err != nil {
			return nil, err
		}
		out.Relationships = append(out.Relationships, pathPlan.Relationships...)
		return out, nil
	}
	out.EvaluationMetrics, err = resolveEvaluationMetrics(model, evaluationNames)
	if err != nil {
		return nil, err
	}
	if err := applyMetricTimeBindings(out); err != nil {
		return nil, err
	}
	if err := validateTimeRelativeBindings(out); err != nil {
		return nil, err
	}
	hasDerived := hasDerivedMetric(out.EvaluationMetrics, model)
	sources, err := model.MetricSources(evaluationNames...)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, invalidQuery("cannot choose metric source root", nil)
	}
	root := sources[0].Dataset
	out.RootDataset = root
	sourceRoots := map[string]struct{}{}
	for _, source := range sources {
		sourceRoots[source.Dataset] = struct{}{}
	}
	// Local, and deliberately not a field.
	//
	// This used to be published as SemanticQuerySpec.RequiresMetricStaging
	// and read by the planner to choose a construction path, which made one bool
	// answer two questions for two layers -- and the planner then wrote true back
	// into it for a condition considered nowhere here, after this function had
	// already acted on the value below. The planner derives its own answer from
	// the nodes construction builds; what is left is this function's own
	// decision about whether a single join tree is worth resolving.
	composedEvaluation := hasDerived || len(sourceRoots) > 1 || hasMetricFilter
	if composedEvaluation {
		if len(out.BooleanFilters) != 0 && len(sourceRoots) > 1 {
			return nil, invalidQuery("boolean OR/NOT predicates require one source population in v1", nil)
		}
		if len(existenceFilters) != 0 && len(sourceRoots) > 1 {
			return nil, unsupportedQueryShape("relationship existence requires one source population", nil)
		}
		if err := resolveRelationshipExistence(model, out, existenceFilters, root); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := resolveRelationshipExistence(model, out, existenceFilters, root); err != nil {
		return nil, err
	}
	required := make([]string, 0, len(neededDatasets))
	for ds := range neededDatasets {
		required = append(required, ds)
	}
	pathPlan, err := model.ResolveDatasetPaths(root, required)
	if err != nil {
		return nil, err
	}
	out.Relationships = append(out.Relationships, pathPlan.Relationships...)
	return out, nil
}

func predicateContainsKind(predicate query.Filter, kind query.PredicateKind) bool {
	if predicate.Kind == kind {
		return true
	}
	for _, child := range predicate.Children {
		if predicateContainsKind(child, kind) {
			return true
		}
	}
	return false
}

func resolveRelationshipExistence(model *manifest.ModelIndex, out *SemanticQuerySpec, predicates []query.Filter, root string) error {
	if len(predicates) == 0 {
		return nil
	}
	if len(predicates) != 1 {
		return unsupportedQueryShape("v1 supports one relationship existence predicate", nil)
	}
	predicate := predicates[0]
	if predicate.Relationship == "" || len(predicate.Children) != 1 {
		return invalidQuery("relationship existence requires relationship and where", nil)
	}
	relationship := model.Relationships[predicate.Relationship]
	if relationship == nil {
		return invalidQuery("relationship not found", map[string]any{"relationship": predicate.Relationship})
	}
	if _, temporal, err := ossie.TemporalRelationship(relationship); err != nil {
		return invalidQuery("relationship existence has invalid relationship metadata", map[string]any{"relationship": predicate.Relationship})
	} else if temporal {
		return unsupportedQueryShape("temporal relationships are not supported by relationship existence v1", map[string]any{"relationship": predicate.Relationship})
	}
	resolved := ResolvedRelationshipExistence{Relationship: relationship, SourceDataset: root}
	switch root {
	case relationship.From:
		resolved.TargetDataset = relationship.To
		resolved.SourceColumns = append([]string(nil), relationship.FromColumns...)
		resolved.TargetColumns = append([]string(nil), relationship.ToColumns...)
	case relationship.To:
		resolved.TargetDataset = relationship.From
		resolved.SourceColumns = append([]string(nil), relationship.ToColumns...)
		resolved.TargetColumns = append([]string(nil), relationship.FromColumns...)
	default:
		return unsupportedQueryShape("relationship existence must be incident to the query source", map[string]any{"relationship": predicate.Relationship, "source_dataset": root})
	}
	if len(resolved.SourceColumns) == 0 || len(resolved.SourceColumns) != len(resolved.TargetColumns) {
		return invalidQuery("relationship existence has incomplete correlation keys", map[string]any{"relationship": predicate.Relationship})
	}
	for _, metric := range out.Metrics {
		for _, dataset := range metric.Datasets {
			if dataset == resolved.TargetDataset {
				return unsupportedQueryShape("relationship existence target cannot contribute metric expressions", map[string]any{"relationship": predicate.Relationship, "metric": metric.Name})
			}
		}
	}
	for _, dimension := range out.Dimensions {
		if dimension.Dataset == resolved.TargetDataset {
			return unsupportedQueryShape("relationship existence target cannot contribute output dimensions", map[string]any{"relationship": predicate.Relationship, "dimension": dimension.Name})
		}
	}
	for _, filter := range out.Filters {
		if filter.Kind == FilterTargetField && filter.Dataset == resolved.TargetDataset {
			return unsupportedQueryShape("relationship existence target filters must be inside where", map[string]any{"relationship": predicate.Relationship, "field": filter.Filter.Field})
		}
	}
	for _, filter := range resolvedPredicateLeaves(out.BooleanFilters) {
		if filter != nil && filter.Dataset == resolved.TargetDataset {
			return unsupportedQueryShape("relationship existence target filters must be inside where", map[string]any{"relationship": predicate.Relationship, "field": filter.Filter.Field})
		}
	}
	for _, order := range out.OrderBy {
		if order.Kind == OrderTargetDimension && order.Dataset == resolved.TargetDataset {
			return unsupportedQueryShape("relationship existence target cannot contribute ordering", map[string]any{"relationship": predicate.Relationship, "field": order.Name})
		}
	}
	targetPredicate, dataset, err := resolveExistenceTargetPredicate(model, predicate.Children[0], resolved.TargetDataset)
	if err != nil {
		return err
	}
	if dataset != resolved.TargetDataset {
		return unsupportedQueryShape("relationship existence where must use the relationship target dataset", map[string]any{"relationship": predicate.Relationship})
	}
	resolved.Predicate = targetPredicate
	out.RelationshipExistence = []ResolvedRelationshipExistence{resolved}
	return nil
}

func resolvedPredicateLeaves(predicates []ResolvedPredicate) []*ResolvedFilter {
	var leaves []*ResolvedFilter
	var visit func(ResolvedPredicate)
	visit = func(predicate ResolvedPredicate) {
		if predicate.Leaf != nil {
			leaves = append(leaves, predicate.Leaf)
			return
		}
		for _, child := range predicate.Children {
			visit(child)
		}
	}
	for _, predicate := range predicates {
		visit(predicate)
	}
	return leaves
}

func resolveExistenceTargetPredicate(model *manifest.ModelIndex, predicate query.Filter, target string) (ResolvedPredicate, string, error) {
	if predicate.Kind == query.PredicateExists {
		return ResolvedPredicate{}, "", unsupportedQueryShape("nested relationship existence is not supported", nil)
	}
	if predicate.Kind == "" {
		if err := validateFilter(predicate); err != nil {
			return ResolvedPredicate{}, "", err
		}
		leaf, err := resolveFilter(model, predicate)
		if err != nil {
			return ResolvedPredicate{}, "", err
		}
		if leaf.Kind != FilterTargetField || leaf.Dataset != target || leaf.Field == nil || leaf.Field.Dimension == nil || isTimeDimension(leaf.Field) {
			return ResolvedPredicate{}, "", unsupportedQueryShape("relationship existence where supports only non-time target dimensions", map[string]any{"field": predicate.Field})
		}
		copy := leaf
		return ResolvedPredicate{Leaf: &copy}, leaf.Dataset, nil
	}
	if predicate.Kind != query.PredicateAnd && predicate.Kind != query.PredicateOr && predicate.Kind != query.PredicateNot {
		return ResolvedPredicate{}, "", invalidQuery("unsupported relationship existence predicate kind", nil)
	}
	if predicate.Kind == query.PredicateNot && len(predicate.Children) != 1 {
		return ResolvedPredicate{}, "", invalidQuery("not predicate requires exactly one child", nil)
	}
	if predicate.Kind != query.PredicateNot && (len(predicate.Children) < 2 || len(predicate.Children) > 32) {
		return ResolvedPredicate{}, "", invalidQuery("and/or predicate requires 2 to 32 children", nil)
	}
	out := ResolvedPredicate{Kind: predicate.Kind}
	for _, child := range predicate.Children {
		resolved, dataset, err := resolveExistenceTargetPredicate(model, child, target)
		if err != nil {
			return ResolvedPredicate{}, "", err
		}
		if dataset != target {
			return ResolvedPredicate{}, "", unsupportedQueryShape("relationship existence where must use one target dataset", nil)
		}
		out.Children = append(out.Children, resolved)
	}
	return out, target, nil
}

func resolveBooleanPredicate(model *manifest.ModelIndex, predicate query.Filter) (ResolvedPredicate, string, error) {
	if predicate.Kind != query.PredicateAnd && predicate.Kind != query.PredicateOr && predicate.Kind != query.PredicateNot {
		return ResolvedPredicate{}, "", invalidQuery("unsupported boolean predicate kind", nil)
	}
	if predicate.Kind == query.PredicateNot && len(predicate.Children) != 1 {
		return ResolvedPredicate{}, "", invalidQuery("not predicate requires exactly one child", nil)
	}
	if predicate.Kind != query.PredicateNot && (len(predicate.Children) < 2 || len(predicate.Children) > 32) {
		return ResolvedPredicate{}, "", invalidQuery("and/or predicate requires 2 to 32 children", nil)
	}
	out := ResolvedPredicate{Kind: predicate.Kind}
	dataset := ""
	for _, child := range predicate.Children {
		if child.Kind != "" {
			resolved, childDataset, err := resolveBooleanPredicate(model, child)
			if err != nil {
				return ResolvedPredicate{}, "", err
			}
			if dataset != "" && dataset != childDataset {
				return ResolvedPredicate{}, "", invalidQuery("boolean predicate leaves must share one dataset in v1", nil)
			}
			dataset = childDataset
			out.Children = append(out.Children, resolved)
			continue
		}
		if err := validateFilter(child); err != nil {
			return ResolvedPredicate{}, "", err
		}
		leaf, err := resolveFilter(model, child)
		if err != nil {
			return ResolvedPredicate{}, "", err
		}
		if leaf.Kind != FilterTargetField {
			return ResolvedPredicate{}, "", invalidQuery("metric filters are not supported inside OR/NOT predicates", nil)
		}
		if isTimeDimension(leaf.Field) {
			return ResolvedPredicate{}, "", invalidQuery("time filters are not supported inside OR/NOT predicates in v1", nil)
		}
		if dataset != "" && dataset != leaf.Dataset {
			return ResolvedPredicate{}, "", invalidQuery("boolean predicate leaves must share one dataset in v1", nil)
		}
		dataset = leaf.Dataset
		copy := leaf
		out.Children = append(out.Children, ResolvedPredicate{Leaf: &copy})
	}
	if dataset == "" {
		return ResolvedPredicate{}, "", invalidQuery("boolean predicate has no filter leaves", nil)
	}
	return out, dataset, nil
}

func validateTimeRelativeBindings(q *SemanticQuerySpec) error {
	if q == nil {
		return nil
	}
	var queryTimeDimensions []string
	for _, d := range q.Dimensions {
		if isTimeDimension(d.Field) {
			queryTimeDimensions = append(queryTimeDimensions, d.Name)
		}
	}
	if len(queryTimeDimensions) == 0 {
		return nil
	}
	for _, metric := range q.EvaluationMetrics {
		if metric.Metric == nil {
			continue
		}
		_, isCumulative, cErr := ossie.CumulativeSpec(metric.Metric)
		if cErr != nil {
			return invalidQuery("invalid cumulative metric extension", map[string]any{"metric": metric.Name, "cause": cErr.Error()})
		}
		_, isOffset, oErr := ossie.TimeOffsetSpec(metric.Metric)
		if oErr != nil {
			return invalidQuery("invalid time-offset metric extension", map[string]any{"metric": metric.Name, "cause": oErr.Error()})
		}
		if !isCumulative && !isOffset {
			continue
		}
		binding := metric.TimeBinding
		if binding == nil {
			return invalidQuery("time-relative metric requires an explicit or inherited canonical time binding", map[string]any{"metric": metric.Name})
		}
		for _, dimension := range queryTimeDimensions {
			if unqualifiedDimensionName(dimension) != binding.TimeDimension {
				return invalidQuery("time-relative metric cannot be evaluated on a non-canonical time dimension", map[string]any{"metric": metric.Name, "canonical_time_dimension": binding.TimeDimension, "query_time_dimension": dimension})
			}
		}
	}
	return nil
}
func unqualifiedDimensionName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i+1:]
		}
	}
	return name
}

func resolveFilter(model *manifest.ModelIndex, f query.Filter) (ResolvedFilter, error) {
	if metric, ok := model.Metrics[f.Field]; ok {
		if !validMetricFilterOperator(f.Operator) {
			return ResolvedFilter{}, invalidQuery("metric filters v1 support gt/gte/lt/lte/between", map[string]any{"metric": f.Field, "operator": f.Operator})
		}
		datasets := metricDatasets(metric, model)
		if len(datasets) == 0 {
			return ResolvedFilter{}, invalidQuery("metric filter dataset cannot be determined from canonical dependency index", map[string]any{"metric": f.Field})
		}
		return ResolvedFilter{Filter: f, Kind: FilterTargetMetric, Metric: metric, MetricDatasets: datasets}, nil
	}
	h, err := resolveField(model, f.Field)
	if err != nil {
		return ResolvedFilter{}, err
	}
	return ResolvedFilter{Filter: f, Kind: FilterTargetField, Dataset: h.Dataset, Field: h.Field}, nil
}
func validMetricFilterOperator(op query.FilterOperator) bool {
	switch op {
	case query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE, query.FilterBetween:
		return true
	default:
		return false
	}
}
func resolveEvaluationMetrics(model *manifest.ModelIndex, names []string) ([]ResolvedMetric, error) {
	if model == nil || model.MetricDependencyGraph == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric dependency graph is not available"}
	}
	order, err := model.MetricDependencyGraph.EvaluationOrder(names...)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedMetric, 0, len(order))
	for _, name := range order {
		metric, err := model.Metric(name)
		if err != nil {
			return nil, err
		}
		datasets := metricDatasets(metric, model)
		if len(datasets) == 0 {
			return nil, invalidQuery("metric dataset cannot be determined from the canonical manifest dependency index; qualify semantic references when scope is ambiguous", map[string]any{"metric": name})
		}
		resolved := ResolvedMetric{Name: name, Metric: metric, Datasets: datasets}
		if binding, ok, err := ossie.MetricTimeBinding(metric); err != nil {
			return nil, invalidQuery("invalid metric time binding", map[string]any{"metric": name, "cause": err.Error()})
		} else if ok {
			resolved.TimeBinding = &binding
		}
		out = append(out, resolved)
	}
	return out, nil
}
func hasDerivedMetric(metrics []ResolvedMetric, model *manifest.ModelIndex) bool {
	for _, metric := range metrics {
		dependency, ok := model.MetricDependency(metric.Name)
		if ok && len(dependency.Metrics) > 0 {
			return true
		}
	}
	return false
}
func metricDatasets(metric *ossie.Metric, model *manifest.ModelIndex) []string {
	if metric == nil || model == nil {
		return nil
	}
	dependency, ok := model.MetricDependency(metric.Name)
	if !ok || len(dependency.Datasets) == 0 {
		return nil
	}
	return append([]string(nil), dependency.Datasets...)
}
func resolveOrderBy(model *manifest.ModelIndex, order query.OrderBy) (ResolvedOrderBy, []string, error) {
	if order.Field == "" {
		return ResolvedOrderBy{}, nil, invalidQuery("order_by field is required", nil)
	}
	if order.Direction != query.SortAsc && order.Direction != query.SortDesc {
		return ResolvedOrderBy{}, nil, invalidQuery("order_by direction must be asc or desc", map[string]any{"field": order.Field, "direction": order.Direction})
	}
	if metric, ok := model.Metrics[order.Field]; ok {
		datasets := metricDatasets(metric, model)
		if len(datasets) == 0 {
			return ResolvedOrderBy{}, nil, invalidQuery("order_by metric dataset cannot be determined from canonical dependency index", map[string]any{"metric": order.Field})
		}
		return ResolvedOrderBy{Name: order.Field, Direction: order.Direction, Kind: OrderTargetMetric, Metric: metric}, datasets, nil
	}
	h, err := model.Dimension(order.Field)
	if err != nil {
		return ResolvedOrderBy{}, nil, invalidQuery("order_by target must resolve to a metric or dimension", map[string]any{"field": order.Field})
	}
	return ResolvedOrderBy{Name: order.Field, Direction: order.Direction, Kind: OrderTargetDimension, Dataset: h.Dataset, Field: h.Field}, []string{h.Dataset}, nil
}
func validateFilter(f query.Filter) error {
	if f.Field == "" {
		return invalidQuery("filter field is required", nil)
	}
	if !validFilterOperator(f.Operator) {
		return invalidQuery("unsupported filter operator", map[string]any{"field": f.Field, "operator": f.Operator})
	}
	switch f.Operator {
	case query.FilterIsNull, query.FilterIsNotNull:
		if f.Value != nil {
			return invalidQuery("null filter operators must not include a value", map[string]any{"field": f.Field, "operator": f.Operator})
		}
	case query.FilterIN, query.FilterNotIn:
		if sliceLen(f.Value) <= 0 {
			return invalidQuery("in/not_in filters require a non-empty array value", map[string]any{"field": f.Field, "operator": f.Operator})
		}
	case query.FilterBetween:
		if sliceLen(f.Value) != 2 {
			return invalidQuery("between filters require exactly two values", map[string]any{"field": f.Field})
		}
	default:
		if f.Value == nil || isSlice(f.Value) || isMap(f.Value) {
			return invalidQuery("comparison filters require a scalar value", map[string]any{"field": f.Field, "operator": f.Operator})
		}
	}
	return nil
}
func validFilterOperator(op query.FilterOperator) bool {
	switch op {
	case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE, query.FilterIN, query.FilterNotIn, query.FilterBetween, query.FilterIsNull, query.FilterIsNotNull:
		return true
	default:
		return false
	}
}
func sliceLen(v any) int {
	if v == nil {
		return -1
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return -1
	}
	return rv.Len()
}
func isSlice(v any) bool { return sliceLen(v) >= 0 }
func isMap(v any) bool {
	if v == nil {
		return false
	}
	return reflect.ValueOf(v).Kind() == reflect.Map
}
func resolveField(model *manifest.ModelIndex, name string) (*manifest.FieldHandle, error) {
	if h, ok := model.Fields[name]; ok {
		return h, nil
	}
	var matches []*manifest.FieldHandle
	for key, h := range model.Fields {
		if len(key) > len(name)+1 && key[len(key)-len(name)-1:] == "."+name {
			matches = append(matches, h)
		}
	}
	if len(matches) == 0 {
		return nil, &serrors.Error{Code: serrors.ErrFieldNotFound, Message: "field not found", Details: map[string]any{"field": name}}
	}
	if len(matches) > 1 {
		return nil, &serrors.Error{Code: serrors.ErrAmbiguousField, Message: "field name is ambiguous; use dataset.field", Details: map[string]any{"field": name}}
	}
	return matches[0], nil
}
func isTimeDimension(f *ossie.Field) bool {
	if f.Dimension == nil {
		return false
	}
	if f.Dimension.IsTime != nil {
		return *f.Dimension.IsTime
	}
	switch f.Datatype {
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}
func validTimeGrain(g query.TimeGrain) bool {
	switch g {
	case query.TimeGrainYear, query.TimeGrainQuarter, query.TimeGrainMonth, query.TimeGrainWeek, query.TimeGrainDay, query.TimeGrainHour:
		return true
	default:
		return false
	}
}
func invalidQuery(message string, details map[string]any) error {
	return &serrors.Error{Code: serrors.ErrInvalidQuery, Message: message, Details: details}
}

func unsupportedQueryShape(message string, details map[string]any) error {
	return &serrors.Error{Code: serrors.ErrUnsupportedQueryShape, Message: message, Details: details}
}
