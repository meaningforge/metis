package semantic

import (
	"context"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

type AssetVisibilityEffect string

const (
	AssetVisibilityVisible AssetVisibilityEffect = "visible"
	AssetVisibilityHidden  AssetVisibilityEffect = "hidden"
)

type AssetVisibilityReason string

const (
	AssetVisibilityReasonDefaultVisible    AssetVisibilityReason = "default_visible"
	AssetVisibilityReasonPolicyVisible     AssetVisibilityReason = "policy_visible"
	AssetVisibilityReasonPolicyHidden      AssetVisibilityReason = "policy_hidden"
	AssetVisibilityReasonPolicyUnavailable AssetVisibilityReason = "policy_unavailable"
	AssetVisibilityReasonInvalidRequest    AssetVisibilityReason = "invalid_request"
	AssetVisibilityReasonInvalidDecision   AssetVisibilityReason = "invalid_decision"
)

// AssetVisibilityRequest deliberately contains canonical, validated evidence
// only. A policy adapter does not receive semantic descriptions or query data.
type AssetVisibilityRequest struct {
	Principal  *auth.Principal
	ProjectID  string
	Action     ProjectAction
	AssetRef   string
	AssetKind  AssetKind
	PolicyTags []string
}

type AssetVisibilityDecision struct {
	Effect AssetVisibilityEffect
	Reason AssetVisibilityReason
}

func (d AssetVisibilityDecision) Visible() bool { return d.Effect == AssetVisibilityVisible }

type AssetVisibilityPolicy interface {
	EvaluateAssetVisibility(context.Context, AssetVisibilityRequest) AssetVisibilityDecision
}

type AssetVisibilityPolicyFunc func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision

func (f AssetVisibilityPolicyFunc) EvaluateAssetVisibility(ctx context.Context, req AssetVisibilityRequest) AssetVisibilityDecision {
	if f == nil {
		return AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonPolicyUnavailable}
	}
	return f(ctx, req)
}

// AllVisibleAssetPolicy is the compatibility-rollout adapter. It is explicit
// and replaceable; malformed requests still fail closed.
type AllVisibleAssetPolicy struct{}

func (AllVisibleAssetPolicy) EvaluateAssetVisibility(_ context.Context, req AssetVisibilityRequest) AssetVisibilityDecision {
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.AssetRef) == "" || !validGovernedAssetKind(req.AssetKind) {
		return AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonInvalidRequest}
	}
	if _, ok := req.Action.RequiredScope(); !ok {
		return AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonInvalidRequest}
	}
	return AssetVisibilityDecision{Effect: AssetVisibilityVisible, Reason: AssetVisibilityReasonDefaultVisible}
}

type AssetGovernanceEvidence struct {
	Owner           string                   `json:"owner,omitempty"`
	Lifecycle       ossie.AssetLifecycle     `json:"lifecycle"`
	Certification   ossie.AssetCertification `json:"certification"`
	DeprecationDate string                   `json:"deprecation_date,omitempty"`
	Replacement     string                   `json:"replacement,omitempty"`
}

func governanceEvidence(extensions []ossie.CustomExtension) (AssetGovernanceEvidence, error) {
	governance, err := ossie.EffectiveAssetGovernance(extensions)
	if err != nil {
		return AssetGovernanceEvidence{}, err
	}
	return AssetGovernanceEvidence{
		Owner: governance.Owner, Lifecycle: governance.Lifecycle, Certification: governance.Certification,
		DeprecationDate: governance.DeprecationDate, Replacement: governance.Replacement,
	}, nil
}

func (s *DiscoveryService) WithAssetVisibilityPolicy(policy AssetVisibilityPolicy) *DiscoveryService {
	if s != nil {
		s.assetVisibility = policy
	}
	return s
}

func (s *DiscoveryService) canAccessAsset(ctx context.Context, projectID string, action ProjectAction, kind AssetKind, ref string, extensions []ossie.CustomExtension) bool {
	if s == nil {
		return false
	}
	governance, err := ossie.EffectiveAssetGovernance(extensions)
	if err != nil {
		return false
	}
	tags := append([]string(nil), governance.PolicyTags...)
	sort.Strings(tags)
	principal, _ := auth.PrincipalFromContext(ctx)
	req := AssetVisibilityRequest{
		Principal: principal, ProjectID: strings.TrimSpace(projectID), Action: action,
		AssetRef: strings.TrimSpace(ref), AssetKind: kind, PolicyTags: tags,
	}
	if req.ProjectID == "" || req.AssetRef == "" || !validGovernedAssetKind(req.AssetKind) {
		return false
	}
	if _, ok := req.Action.RequiredScope(); !ok {
		return false
	}
	decision := AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonPolicyUnavailable}
	if s.assetVisibility != nil {
		decision = s.assetVisibility.EvaluateAssetVisibility(ctx, req)
	}
	if decision.Effect != AssetVisibilityVisible && decision.Effect != AssetVisibilityHidden {
		return false
	}
	if !validAssetVisibilityDecision(decision) {
		return false
	}
	return decision.Visible()
}

func validGovernedAssetKind(kind AssetKind) bool {
	switch kind {
	case AssetModel, AssetDataset, AssetMetric, AssetDimension, AssetRelationship:
		return true
	default:
		return false
	}
}

func validAssetVisibilityReason(reason AssetVisibilityReason) bool {
	switch reason {
	case AssetVisibilityReasonDefaultVisible, AssetVisibilityReasonPolicyVisible, AssetVisibilityReasonPolicyHidden,
		AssetVisibilityReasonPolicyUnavailable, AssetVisibilityReasonInvalidRequest, AssetVisibilityReasonInvalidDecision:
		return true
	default:
		return false
	}
}

func validAssetVisibilityDecision(decision AssetVisibilityDecision) bool {
	if !validAssetVisibilityReason(decision.Reason) {
		return false
	}
	switch decision.Effect {
	case AssetVisibilityVisible:
		return decision.Reason == AssetVisibilityReasonDefaultVisible || decision.Reason == AssetVisibilityReasonPolicyVisible
	case AssetVisibilityHidden:
		return decision.Reason != AssetVisibilityReasonDefaultVisible && decision.Reason != AssetVisibilityReasonPolicyVisible
	default:
		return false
	}
}

func modelAssetRef(model string) string            { return "model:" + model }
func datasetAssetRef(model, dataset string) string { return "dataset:" + model + "." + dataset }
func metricAssetRef(model, metric string) string   { return "metric:" + model + "." + metric }
func dimensionAssetRef(model, dataset, dimension string) string {
	return "dimension:" + model + "." + dataset + "." + dimension
}
func relationshipAssetRef(model, relationship string) string {
	return "relationship:" + model + "." + relationship
}

// authorizeSemanticQueryAssets is the shared direct-reference gate used by
// compile and execution paths after project authorization. Denials intentionally
// use the ordinary not-found contracts so hidden inventory is not disclosed.
func (s *DiscoveryService) authorizeSemanticQueryAssets(ctx context.Context, semanticQuery query.SemanticQuery, action ProjectAction) error {
	if s == nil || s.manifest == nil {
		return nil
	}
	lookup, err := s.manifest.Lookup()
	if err != nil {
		return err
	}
	if lookup == nil {
		return nil
	}
	projectLookup, err := lookup.Project(semanticQuery.Project)
	if err != nil {
		return err
	}
	modelIndex, err := projectLookup.Model(semanticQuery.Model)
	if err != nil {
		return err
	}
	model := modelIndex.ManifestModel()
	if model == nil || model.Model == nil || !s.canAccessAsset(ctx, semanticQuery.Project, action, AssetModel, modelAssetRef(semanticQuery.Model), model.Model.CustomExtensions) {
		return modelNotFound(semanticQuery.Model)
	}
	checkedMetrics := make(map[string]struct{})
	checkedDimensions := make(map[string]struct{})
	sourceDatasets := make(map[string]struct{})
	targetDimensions := make(map[string]string)
	checkDataset := func(name string) bool {
		dataset := model.Datasets[name]
		return dataset != nil && s.canAccessAsset(ctx, semanticQuery.Project, action, AssetDataset, datasetAssetRef(semanticQuery.Model, name), dataset.CustomExtensions)
	}
	checkMetric := func(name string) error {
		name = strings.TrimSpace(name)
		if _, ok := checkedMetrics[name]; ok {
			return nil
		}
		metric := model.Metrics[name]
		if metric == nil {
			return nil // Resolver owns existence diagnostics.
		}
		if !s.canAccessAsset(ctx, semanticQuery.Project, action, AssetMetric, metricAssetRef(semanticQuery.Model, name), metric.CustomExtensions) {
			return &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": semanticQuery.Model, "metric": name}}
		}
		if model.MetricDependencyGraph != nil {
			order, orderErr := model.MetricDependencyGraph.EvaluationOrder(name)
			if orderErr == nil {
				for _, dependencyName := range order {
					dependency := model.Metrics[dependencyName]
					if dependency == nil || !s.canAccessAsset(ctx, semanticQuery.Project, action, AssetMetric, metricAssetRef(semanticQuery.Model, dependencyName), dependency.CustomExtensions) {
						return &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": semanticQuery.Model, "metric": name}}
					}
				}
			}
		}
		sources, sourceErr := model.MetricSources(name)
		if sourceErr == nil {
			for _, source := range sources {
				if !checkDataset(source.Dataset) {
					return &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": semanticQuery.Model, "metric": name}}
				}
				sourceDatasets[source.Dataset] = struct{}{}
			}
		}
		checkedMetrics[name] = struct{}{}
		return nil
	}
	checkDimension := func(name string) error {
		name = strings.TrimSpace(name)
		if _, ok := checkedDimensions[name]; ok {
			return nil
		}
		handle, dimensionErr := model.Dimension(name)
		if dimensionErr != nil {
			return nil // Resolver owns existence and ambiguity diagnostics.
		}
		if !checkDataset(handle.Dataset) || !s.canAccessAsset(ctx, semanticQuery.Project, action, AssetDimension, dimensionAssetRef(semanticQuery.Model, handle.Dataset, handle.Field.Name), handle.Field.CustomExtensions) {
			return &serrors.Error{Code: serrors.ErrDimensionNotFound, Message: "dimension not found", Details: map[string]any{"model": semanticQuery.Model, "dimension": name}}
		}
		targetDimensions[handle.Dataset] = name
		checkedDimensions[name] = struct{}{}
		return nil
	}
	for _, metric := range semanticQuery.Metrics {
		if err := checkMetric(metric.Name); err != nil {
			return err
		}
	}
	for _, dimension := range semanticQuery.Dimensions {
		if err := checkDimension(dimension.Name); err != nil {
			return err
		}
	}
	for _, field := range semanticQueryFields(semanticQuery) {
		if model.Metrics[field] != nil {
			if err := checkMetric(field); err != nil {
				return err
			}
			continue
		}
		if err := checkDimension(field); err != nil {
			return err
		}
	}
	for source := range sourceDatasets {
		for target, dimensionName := range targetDimensions {
			plan, pathErr := model.ResolveDatasetPaths(source, []string{target})
			if pathErr != nil {
				continue
			}
			for _, relationship := range datasetPath(plan, target) {
				if relationship == nil || !s.canAccessAsset(ctx, semanticQuery.Project, action, AssetRelationship, relationshipAssetRef(semanticQuery.Model, relationship.Name), relationship.CustomExtensions) {
					return &serrors.Error{Code: serrors.ErrDimensionNotFound, Message: "dimension not found", Details: map[string]any{"model": semanticQuery.Model, "dimension": dimensionName}}
				}
			}
		}
	}
	return nil
}

func semanticQueryFields(semanticQuery query.SemanticQuery) []string {
	fields := make([]string, 0, len(semanticQuery.Filters)+len(semanticQuery.OrderBy))
	for _, filter := range semanticQuery.Filters {
		fields = append(fields, filter.Field)
	}
	for _, order := range semanticQuery.OrderBy {
		fields = append(fields, order.Field)
	}
	return fields
}

func (s *DiscoveryService) visibleMetric(ctx context.Context, projectID string, action ProjectAction, modelName, metricName string) bool {
	return s != nil && s.authorizeSemanticQueryAssets(ctx, query.SemanticQuery{
		Project: projectID, Model: modelName, Metrics: []query.MetricRef{{Name: metricName}},
	}, action) == nil
}

// indexedMetricVisible applies the same metric/dependency/source visibility
// boundary as visibleMetric without rebuilding a SemanticQuery lookup. The
// required canonical keys were derived from the immutable generation.
func (s *DiscoveryService) indexedMetricVisible(ctx context.Context, projectID string, model *manifest.ModelIndex, metric indexedMetric) bool {
	if s == nil || model == nil {
		return false
	}
	for _, name := range metric.requiredMetrics {
		candidate := model.Metrics[name]
		if candidate == nil || !s.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetMetric, metricAssetRef(metric.model, name), candidate.CustomExtensions) {
			return false
		}
	}
	for _, name := range metric.sourceDatasets {
		dataset := model.Datasets[name]
		if dataset == nil || !s.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(metric.model, name), dataset.CustomExtensions) {
			return false
		}
	}
	return true
}

const AssetGovernanceWarningDeprecated = "SEMANTIC_ASSET_DEPRECATED"

func (s *DiscoveryService) semanticQueryGovernanceWarnings(semanticQuery query.SemanticQuery) ([]artifact.Warning, error) {
	if s == nil || s.manifest == nil {
		return nil, nil
	}
	lookup, err := s.manifest.Lookup()
	if err != nil || lookup == nil {
		return nil, err
	}
	projectLookup, err := lookup.Project(semanticQuery.Project)
	if err != nil {
		return nil, err
	}
	lookupModel, err := projectLookup.Model(semanticQuery.Model)
	if err != nil {
		return nil, err
	}
	model := lookupModel.ManifestModel()
	warnings := make(map[string]artifact.Warning)
	sourceDatasets := make(map[string]struct{})
	targetDatasets := make(map[string]struct{})
	appendWarning := func(ref string, extensions []ossie.CustomExtension) error {
		governance, governanceErr := ossie.EffectiveAssetGovernance(extensions)
		if governanceErr != nil {
			return governanceErr
		}
		if governance.Lifecycle == ossie.AssetLifecycleDeprecated {
			warnings[ref] = artifact.Warning{Code: AssetGovernanceWarningDeprecated, AssetRef: ref, DeprecationDate: governance.DeprecationDate, Replacement: governance.Replacement}
		}
		return nil
	}
	if model == nil || model.Model == nil {
		return nil, nil
	}
	if err := appendWarning(modelAssetRef(model.Model.Name), model.Model.CustomExtensions); err != nil {
		return nil, err
	}
	appendDataset := func(name string) error {
		if dataset := model.Datasets[name]; dataset != nil {
			return appendWarning(datasetAssetRef(model.Model.Name, name), dataset.CustomExtensions)
		}
		return nil
	}
	appendMetric := func(name string) error {
		metric := model.Metrics[name]
		if metric == nil {
			return nil
		}
		if err := appendWarning(metricAssetRef(model.Model.Name, name), metric.CustomExtensions); err != nil {
			return err
		}
		if model.MetricDependencyGraph != nil {
			order, orderErr := model.MetricDependencyGraph.EvaluationOrder(name)
			if orderErr == nil {
				for _, dependencyName := range order {
					dependency := model.Metrics[dependencyName]
					if dependency != nil {
						if err := appendWarning(metricAssetRef(model.Model.Name, dependencyName), dependency.CustomExtensions); err != nil {
							return err
						}
					}
				}
			}
		}
		if sources, sourceErr := model.MetricSources(name); sourceErr == nil {
			for _, source := range sources {
				sourceDatasets[source.Dataset] = struct{}{}
				if err := appendDataset(source.Dataset); err != nil {
					return err
				}
			}
		}
		return nil
	}
	appendDimension := func(name string) error {
		handle, dimensionErr := model.Dimension(name)
		if dimensionErr != nil || handle == nil || handle.Field == nil {
			return nil
		}
		if err := appendDataset(handle.Dataset); err != nil {
			return err
		}
		targetDatasets[handle.Dataset] = struct{}{}
		return appendWarning(dimensionAssetRef(model.Model.Name, handle.Dataset, handle.Field.Name), handle.Field.CustomExtensions)
	}
	for _, metric := range semanticQuery.Metrics {
		if err := appendMetric(metric.Name); err != nil {
			return nil, err
		}
	}
	for _, dimension := range semanticQuery.Dimensions {
		if err := appendDimension(dimension.Name); err != nil {
			return nil, err
		}
	}
	for _, field := range semanticQueryFields(semanticQuery) {
		if model.Metrics[field] != nil {
			if err := appendMetric(field); err != nil {
				return nil, err
			}
		} else if err := appendDimension(field); err != nil {
			return nil, err
		}
	}
	for source := range sourceDatasets {
		for target := range targetDatasets {
			plan, pathErr := model.ResolveDatasetPaths(source, []string{target})
			if pathErr != nil {
				continue
			}
			for _, relationship := range datasetPath(plan, target) {
				if relationship != nil {
					if err := appendWarning(relationshipAssetRef(model.Model.Name, relationship.Name), relationship.CustomExtensions); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	refs := make([]string, 0, len(warnings))
	for ref := range warnings {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	result := make([]artifact.Warning, 0, len(refs))
	for _, ref := range refs {
		result = append(result, warnings[ref])
	}
	return result, nil
}
