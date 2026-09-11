package semantic

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const (
	defaultCompatibleDimensionsLimit = 20
	maxCompatibleDimensionsLimit     = 100
	semanticContextCursorPrefix      = "dimension:v1:"
)

// SemanticContextService exposes a bounded, transport-neutral semantic context
// bundle for Agent reasoning. It derives all facts from the current SemanticManifest
// manifest and reuses the same relationship compatibility contract as
// DiscoveryService.
type SemanticContextService struct {
	manifest  *manifest.Store
	discovery *DiscoveryService
}

func NewSemanticContextService(store *manifest.Store) *SemanticContextService {
	discovery := NewDiscoveryService(store)
	return &SemanticContextService{manifest: store, discovery: discovery}
}

func (s *SemanticContextService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *SemanticContextService {
	if s != nil && s.discovery != nil {
		s.discovery.WithProjectAuthorizer(authorizer)
	}
	return s
}

type SemanticContextRequest struct {
	Project                  string                           `json:"project"`
	Model                    string                           `json:"model"`
	Metrics                  []string                         `json:"metrics,omitempty"`
	Dimensions               []string                         `json:"dimensions,omitempty"`
	CompatibleDimensionsPage *CompatibleDimensionsPageRequest `json:"compatible_dimensions_page,omitempty"`
}

type CompatibleDimensionsPageRequest struct {
	Metric string `json:"metric,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type SemanticContext struct {
	Project                  string                    `json:"project"`
	Model                    string                    `json:"model"`
	Metrics                  []MetricContext           `json:"metrics,omitempty"`
	Dimensions               []DimensionContext        `json:"dimensions,omitempty"`
	Relationships            []RelationshipContext     `json:"relationships,omitempty"`
	CompatibleDimensionsPage *CompatibleDimensionsPage `json:"compatible_dimensions_page,omitempty"`
}

type MetricContext struct {
	Name                string                     `json:"name"`
	Description         string                     `json:"description,omitempty"`
	Datatype            ossie.DataType             `json:"datatype,omitempty"`
	DirectDependencies  []string                   `json:"direct_dependencies,omitempty"`
	Dependencies        []string                   `json:"dependencies,omitempty"`
	SourceDatasets      []string                   `json:"source_datasets,omitempty"`
	SemanticConstraints []MetricSemanticConstraint `json:"semantic_constraints,omitempty"`
}

type DimensionContext struct {
	Name          string                         `json:"name"`
	QualifiedName string                         `json:"qualified_name"`
	Dataset       string                         `json:"dataset"`
	Description   string                         `json:"description,omitempty"`
	Datatype      ossie.DataType                 `json:"datatype,omitempty"`
	Compatibility []DimensionMetricCompatibility `json:"compatibility,omitempty"`
}

type DimensionMetricCompatibility struct {
	Metric string                `json:"metric"`
	Status CompatibilityStatus   `json:"status"`
	Paths  []DimensionSourcePath `json:"relationship_paths,omitempty"`
	Issues []DimensionPathIssue  `json:"issues,omitempty"`
}

type RelationshipContext struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type CompatibleDimensionsPage struct {
	Metric     string             `json:"metric"`
	Items      []DimensionContext `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type MetricSemanticConstraintKind string

const (
	MetricConstraintConversion       MetricSemanticConstraintKind = "conversion"
	MetricConstraintCumulative       MetricSemanticConstraintKind = "cumulative"
	MetricConstraintDefinitionFilter MetricSemanticConstraintKind = "definition_filter"
	MetricConstraintFill             MetricSemanticConstraintKind = "fill"
	MetricConstraintOffsetToGrain    MetricSemanticConstraintKind = "offset_to_grain"
	MetricConstraintSemiAdditive     MetricSemanticConstraintKind = "semi_additive"
	MetricConstraintTimeBinding      MetricSemanticConstraintKind = "time_binding"
	MetricConstraintTimeOffset       MetricSemanticConstraintKind = "time_offset"
)

type MetricSemanticConstraint struct {
	Kind             MetricSemanticConstraintKind `json:"kind"`
	Conversion       *ConversionConstraint        `json:"conversion,omitempty"`
	Cumulative       *CumulativeConstraint        `json:"cumulative,omitempty"`
	DefinitionFilter *DefinitionFilterConstraint  `json:"definition_filter,omitempty"`
	Fill             *FillConstraint              `json:"fill,omitempty"`
	OffsetToGrain    *OffsetToGrainConstraint     `json:"offset_to_grain,omitempty"`
	SemiAdditive     *SemiAdditiveConstraint      `json:"semi_additive,omitempty"`
	TimeBinding      *TimeBindingConstraint       `json:"time_binding,omitempty"`
	TimeOffset       *TimeOffsetConstraint        `json:"time_offset,omitempty"`
}

type ConversionConstraint struct {
	BaseMetric         string                         `json:"base_metric"`
	ConversionMetric   string                         `json:"conversion_metric"`
	Calculation        string                         `json:"calculation"`
	Entity             ConversionPropertyConstraint   `json:"entity"`
	Window             *ConversionWindowConstraint    `json:"window,omitempty"`
	ConstantProperties []ConversionPropertyConstraint `json:"constant_properties,omitempty"`
}

type ConversionPropertyConstraint struct {
	BaseProperty       string `json:"base_property"`
	ConversionProperty string `json:"conversion_property"`
}

type ConversionWindowConstraint struct {
	Count int    `json:"count"`
	Unit  string `json:"unit"`
}

type CumulativeConstraint struct {
	BaseMetric    string `json:"base_metric"`
	TimeDimension string `json:"time_dimension"`
	WindowType    string `json:"window_type"`
	Count         int    `json:"count,omitempty"`
	Unit          string `json:"unit,omitempty"`
}

type DefinitionFilterConstraint struct {
	Stage   string         `json:"stage"`
	Filters []query.Filter `json:"filters"`
}

type FillConstraint struct {
	Policy ossie.MetricFillPolicy `json:"policy"`
}

type OffsetToGrainConstraint struct {
	BaseMetric    string `json:"base_metric"`
	TimeDimension string `json:"time_dimension"`
	Grain         string `json:"grain"`
}

type SemiAdditiveConstraint struct {
	BaseMetric           string   `json:"base_metric"`
	NonAdditiveDimension string   `json:"non_additive_dimension"`
	Selector             string   `json:"selector"`
	TieBreakDimension    string   `json:"tie_break_dimension,omitempty"`
	NullPolicy           string   `json:"null_policy,omitempty"`
	WindowGroupings      []string `json:"window_groupings,omitempty"`
	RollupAggregation    string   `json:"rollup_aggregation,omitempty"`
}

type TimeBindingConstraint struct {
	TimeDimension string `json:"time_dimension"`
}

type TimeOffsetConstraint struct {
	BaseMetric    string `json:"base_metric"`
	TimeDimension string `json:"time_dimension"`
	Count         int    `json:"count"`
	Unit          string `json:"unit"`
}

func (s *SemanticContextService) Get(ctx context.Context, req SemanticContextRequest) (*SemanticContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.manifest == nil {
		return nil, fmt.Errorf("semantic context service manifest is not configured")
	}
	project, err := s.discovery.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project

	metrics, err := semanticContextFocus(req.Metrics, "metrics")
	if err != nil {
		return nil, err
	}
	dimensions, err := semanticContextFocus(req.Dimensions, "dimensions")
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 && len(dimensions) == 0 {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "semantic context requires at least one metric or dimension"}
	}

	discovery := s.discovery
	idx, err := discovery.model(ctx, req.Project, req.Model)
	if err != nil {
		return nil, err
	}

	result := &SemanticContext{Project: req.Project, Model: req.Model}
	relationshipNames := map[string]struct{}{}
	compatibilityCache := map[string]*MetricDimensionCompatibilityResult{}
	compatibilityForMetric := func(metric string) (*MetricDimensionCompatibilityResult, error) {
		if cached, ok := compatibilityCache[metric]; ok {
			return cached, nil
		}
		compatibility, err := discovery.getMetricDimensions(ctx, req.Project, req.Model, metric)
		if err != nil {
			return nil, err
		}
		compatibilityCache[metric] = compatibility
		return compatibility, nil
	}

	for _, metricName := range metrics {
		metric, err := idx.Metric(metricName)
		if err != nil {
			return nil, err
		}
		direct, dependencies, err := semanticMetricDependencies(idx, metricName)
		if err != nil {
			return nil, err
		}
		sources, err := semanticMetricSourceDatasets(idx, metricName)
		if err != nil {
			return nil, err
		}
		constraints, err := metricSemanticConstraints(metric)
		if err != nil {
			return nil, err
		}
		result.Metrics = append(result.Metrics, MetricContext{
			Name:                metric.Name,
			Description:         metric.Description,
			Datatype:            metric.Datatype,
			DirectDependencies:  direct,
			Dependencies:        dependencies,
			SourceDatasets:      sources,
			SemanticConstraints: constraints,
		})
	}

	for _, dimensionName := range dimensions {
		handle, err := idx.Dimension(dimensionName)
		if err != nil {
			return nil, err
		}
		dimension := dimensionContext(handle)
		for _, metricName := range metrics {
			compatibility, err := compatibilityForMetric(metricName)
			if err != nil {
				return nil, err
			}
			item, ok := findDimensionCompatibility(compatibility, dimension.QualifiedName)
			if !ok {
				return nil, &serrors.Error{
					Code:    serrors.ErrInvalidModel,
					Message: "dimension compatibility is missing from manifest discovery",
					Details: map[string]any{"metric": metricName, "dimension": dimension.QualifiedName},
				}
			}
			dimension.Compatibility = append(dimension.Compatibility, dimensionMetricCompatibility(metricName, item))
			collectRelationshipNames(relationshipNames, item.Paths)
		}
		result.Dimensions = append(result.Dimensions, dimension)
	}

	if req.CompatibleDimensionsPage != nil {
		pageMetric, err := semanticContextPageMetric(metrics, req.CompatibleDimensionsPage.Metric)
		if err != nil {
			return nil, err
		}
		limit, err := semanticContextPageLimit(req.CompatibleDimensionsPage.Limit)
		if err != nil {
			return nil, err
		}
		after, err := decodeSemanticContextCursor(req.CompatibleDimensionsPage.Cursor)
		if err != nil {
			return nil, err
		}
		compatibility, err := compatibilityForMetric(pageMetric)
		if err != nil {
			return nil, err
		}
		page, err := semanticContextCompatibleDimensionsPage(idx, pageMetric, compatibility, limit, after, relationshipNames)
		if err != nil {
			return nil, err
		}
		result.CompatibleDimensionsPage = page
	}

	result.Relationships = semanticContextRelationships(idx, relationshipNames)
	return result, nil
}

func semanticContextFocus(values []string, field string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "semantic context focus contains an empty name", Details: map[string]any{"field": field}}
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func semanticMetricDependencies(idx *manifest.ModelIndex, metric string) ([]string, []string, error) {
	if idx == nil || idx.MetricDependencyGraph == nil {
		return nil, nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric dependency graph is not available", Details: map[string]any{"metric": metric}}
	}
	direct, ok := idx.MetricDependencyGraph.DirectDependencies(metric)
	if !ok {
		return nil, nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": metric}}
	}
	order, err := idx.MetricDependencyGraph.EvaluationOrder(metric)
	if err != nil {
		return nil, nil, err
	}
	required := make([]string, 0, len(order))
	for _, name := range order {
		if name != metric {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return direct, required, nil
}

func semanticMetricSourceDatasets(idx *manifest.ModelIndex, metric string) ([]string, error) {
	sources, err := idx.MetricSources(metric)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, source := range sources {
		set[source.Dataset] = struct{}{}
	}
	return sortedStrings(set), nil
}

func dimensionContext(handle *manifest.FieldHandle) DimensionContext {
	return DimensionContext{
		Name:          handle.Field.Name,
		QualifiedName: handle.Dataset + "." + handle.Field.Name,
		Dataset:       handle.Dataset,
		Description:   handle.Field.Description,
		Datatype:      handle.Field.Datatype,
	}
}

func findDimensionCompatibility(result *MetricDimensionCompatibilityResult, qualified string) (DimensionCompatibility, bool) {
	if result == nil {
		return DimensionCompatibility{}, false
	}
	for _, dimension := range result.Dimensions {
		if dimension.Qualified == qualified {
			return dimension, true
		}
	}
	return DimensionCompatibility{}, false
}

func dimensionMetricCompatibility(metric string, dimension DimensionCompatibility) DimensionMetricCompatibility {
	return DimensionMetricCompatibility{
		Metric: metric,
		Status: dimension.Status,
		Paths:  append([]DimensionSourcePath(nil), dimension.Paths...),
		Issues: append([]DimensionPathIssue(nil), dimension.Issues...),
	}
}

func collectRelationshipNames(names map[string]struct{}, paths []DimensionSourcePath) {
	for _, path := range paths {
		for _, relationship := range path.Relationships {
			names[relationship] = struct{}{}
		}
	}
}

func semanticContextPageMetric(metrics []string, requested string) (string, error) {
	metric := strings.TrimSpace(requested)
	if metric == "" {
		if len(metrics) != 1 {
			return "", &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compatible dimension pagination requires metric when multiple metrics are focused"}
		}
		return metrics[0], nil
	}
	for _, focused := range metrics {
		if focused == metric {
			return metric, nil
		}
	}
	return "", &serrors.Error{
		Code:    serrors.ErrInvalidQuery,
		Message: "compatible dimension pagination metric must be included in semantic context metrics",
		Details: map[string]any{"metric": metric},
	}
}

func semanticContextPageLimit(requested int) (int, error) {
	if requested == 0 {
		return defaultCompatibleDimensionsLimit, nil
	}
	if requested < 0 || requested > maxCompatibleDimensionsLimit {
		return 0, &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: "compatible dimension page limit is out of range",
			Details: map[string]any{"limit": requested, "max": maxCompatibleDimensionsLimit},
		}
	}
	return requested, nil
}

func encodeSemanticContextCursor(qualified string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(semanticContextCursorPrefix + qualified))
}

func decodeSemanticContextCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", invalidSemanticContextCursor()
	}
	value := string(decoded)
	if !strings.HasPrefix(value, semanticContextCursorPrefix) || len(value) == len(semanticContextCursorPrefix) {
		return "", invalidSemanticContextCursor()
	}
	return strings.TrimPrefix(value, semanticContextCursorPrefix), nil
}

func invalidSemanticContextCursor() error {
	return &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compatible dimension cursor is invalid", Details: map[string]any{"field": "compatible_dimensions_page.cursor"}}
}

func semanticContextCompatibleDimensionsPage(
	idx *manifest.ModelIndex,
	metric string,
	compatibility *MetricDimensionCompatibilityResult,
	limit int,
	after string,
	relationshipNames map[string]struct{},
) (*CompatibleDimensionsPage, error) {
	compatible := make([]DimensionCompatibility, 0, len(compatibility.Dimensions))
	for _, dimension := range compatibility.Dimensions {
		if dimension.Status != CompatibilityCompatible || dimension.Qualified <= after {
			continue
		}
		compatible = append(compatible, dimension)
	}

	page := &CompatibleDimensionsPage{Metric: metric}
	pageSize := limit
	if len(compatible) < pageSize {
		pageSize = len(compatible)
	}
	for _, item := range compatible[:pageSize] {
		handle, err := idx.Dimension(item.Qualified)
		if err != nil {
			return nil, err
		}
		dimension := dimensionContext(handle)
		dimension.Compatibility = []DimensionMetricCompatibility{dimensionMetricCompatibility(metric, item)}
		page.Items = append(page.Items, dimension)
		collectRelationshipNames(relationshipNames, item.Paths)
	}
	if len(compatible) > pageSize && pageSize > 0 {
		page.NextCursor = encodeSemanticContextCursor(compatible[pageSize-1].Qualified)
	}
	return page, nil
}

func semanticContextRelationships(idx *manifest.ModelIndex, names map[string]struct{}) []RelationshipContext {
	ordered := sortedStrings(names)
	out := make([]RelationshipContext, 0, len(ordered))
	for _, name := range ordered {
		relationship := idx.Relationships[name]
		if relationship == nil {
			continue
		}
		out = append(out, RelationshipContext{Name: relationship.Name, From: relationship.From, To: relationship.To})
	}
	return out
}

func metricSemanticConstraints(metric *ossie.Metric) ([]MetricSemanticConstraint, error) {
	var constraints []MetricSemanticConstraint

	conversion, ok, err := ossie.ConversionSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintConversion, err)
	}
	if ok {
		constraint := &ConversionConstraint{
			BaseMetric:       conversion.BaseMetric,
			ConversionMetric: conversion.ConversionMetric,
			Calculation:      conversion.Calculation,
			Entity: ConversionPropertyConstraint{
				BaseProperty: conversion.Entity.BaseProperty, ConversionProperty: conversion.Entity.ConversionProperty,
			},
		}
		if conversion.Window != nil {
			constraint.Window = &ConversionWindowConstraint{Count: conversion.Window.Count, Unit: conversion.Window.Unit}
		}
		for _, property := range conversion.ConstantProperties {
			constraint.ConstantProperties = append(constraint.ConstantProperties, ConversionPropertyConstraint{
				BaseProperty: property.BaseProperty, ConversionProperty: property.ConversionProperty,
			})
		}
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintConversion, Conversion: constraint})
	}

	cumulative, ok, err := ossie.CumulativeSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintCumulative, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintCumulative, Cumulative: &CumulativeConstraint{
			BaseMetric: cumulative.BaseMetric, TimeDimension: cumulative.TimeDimension,
			WindowType: cumulative.Window.Type, Count: cumulative.Window.Count, Unit: cumulative.Window.Unit,
		}})
	}

	definitionFilter, ok, err := ossie.MetricDefinitionFilter(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintDefinitionFilter, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintDefinitionFilter, DefinitionFilter: &DefinitionFilterConstraint{
			Stage: ossie.EffectiveMetricDefinitionFilterStage(definitionFilter), Filters: append([]query.Filter(nil), definitionFilter.Filters...),
		}})
	}

	fill, ok, err := ossie.FillSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintFill, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintFill, Fill: &FillConstraint{Policy: fill.Policy}})
	}

	offsetToGrain, ok, err := ossie.OffsetToGrainSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintOffsetToGrain, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintOffsetToGrain, OffsetToGrain: &OffsetToGrainConstraint{
			BaseMetric: offsetToGrain.BaseMetric, TimeDimension: offsetToGrain.TimeDimension, Grain: offsetToGrain.Grain,
		}})
	}

	semiAdditive, ok, err := ossie.SemiAdditiveSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintSemiAdditive, err)
	}
	if ok {
		rollup := ""
		if len(semiAdditive.WindowGroupings) > 0 {
			rollup = ossie.EffectiveSemiAdditiveRollupAggregation(semiAdditive)
		}
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintSemiAdditive, SemiAdditive: &SemiAdditiveConstraint{
			BaseMetric: semiAdditive.BaseMetric, NonAdditiveDimension: semiAdditive.NonAdditiveDimension,
			Selector: semiAdditive.Aggregation, TieBreakDimension: semiAdditive.TieBreakDimension,
			NullPolicy: semiAdditive.NullPolicy, WindowGroupings: append([]string(nil), semiAdditive.WindowGroupings...),
			RollupAggregation: rollup,
		}})
	}

	timeBinding, ok, err := ossie.MetricTimeBinding(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintTimeBinding, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintTimeBinding, TimeBinding: &TimeBindingConstraint{TimeDimension: timeBinding.TimeDimension}})
	}

	timeOffset, ok, err := ossie.TimeOffsetSpec(metric)
	if err != nil {
		return nil, semanticConstraintError(metric, MetricConstraintTimeOffset, err)
	}
	if ok {
		constraints = append(constraints, MetricSemanticConstraint{Kind: MetricConstraintTimeOffset, TimeOffset: &TimeOffsetConstraint{
			BaseMetric: timeOffset.BaseMetric, TimeDimension: timeOffset.TimeDimension,
			Count: timeOffset.Offset.Count, Unit: timeOffset.Offset.Unit,
		}})
	}

	sort.Slice(constraints, func(i, j int) bool { return constraints[i].Kind < constraints[j].Kind })
	return constraints, nil
}

func semanticConstraintError(metric *ossie.Metric, kind MetricSemanticConstraintKind, cause error) error {
	name := ""
	if metric != nil {
		name = metric.Name
	}
	return &serrors.Error{
		Code:    serrors.ErrInvalidModel,
		Message: "invalid metric semantic constraint",
		Details: map[string]any{"metric": name, "kind": kind, "cause": cause.Error()},
	}
}
