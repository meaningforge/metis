package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

// AgentSemanticService is the primary Agent discovery facade. It exposes
// compact indexes plus explicit detail tools instead of one generic context dump.
type AgentSemanticService struct {
	discovery    *DiscoveryService
	capabilities AgentProjectCapabilityProvider
}

const maxAgentSearchTerms = 20

func NewAgentSemanticService(discovery *DiscoveryService, providers ...AgentProjectCapabilityProvider) *AgentSemanticService {
	service := &AgentSemanticService{discovery: discovery}
	if len(providers) > 0 {
		service.capabilities = providers[0]
	}
	return service
}

// WithProjectAuthorizer installs the deployment's project-access decision.
// The local single-key deployment authorizes every configured project; SaaS
// deployments can inject tenant/project membership without changing MCP DTOs.
func (s *AgentSemanticService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *AgentSemanticService {
	if s != nil && s.discovery != nil {
		s.discovery.WithProjectAuthorizer(authorizer)
	}
	return s
}

type ListProjectsRequest struct {
	ProjectIDs []string `json:"project_ids,omitempty" jsonschema:"Optional exact project IDs to return; omit to list every visible project."`
}

type AgentModelSummary struct {
	Ref         string                  `json:"ref"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Governance  AssetGovernanceEvidence `json:"governance"`
}

type AgentProjectSummary struct {
	ProjectID    string                   `json:"project_id"`
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	Capabilities []AgentProjectCapability `json:"capabilities,omitempty"`
}

// AgentProjectCapability is a closed operation-level capability advertised
// for one visible Project. It exposes no DataSource or placement identity.
type AgentProjectCapability string

const (
	AgentCapabilityCompileSQL   AgentProjectCapability = "compile_sql"
	AgentCapabilityQueryMetrics AgentProjectCapability = "query_metrics"
)

// AgentProjectCapabilityProvider reports deployment capabilities without
// opening a Driver or executing a query.
type AgentProjectCapabilityProvider interface {
	ProjectCapabilities(context.Context, string) []AgentProjectCapability
}

type ListProjectsResult struct {
	Projects []AgentProjectSummary `json:"projects"`
}

type ListModelsRequest struct {
	ProjectID string   `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Search    []string `json:"search,omitempty" jsonschema:"Optional terms matched against model identity and contained dataset, metric, and dimension names."`
	Limit     *int     `json:"limit,omitempty" jsonschema:"Maximum models to return; defaults to 10 and cannot exceed 50."`
	Cursor    string   `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by an earlier request with the same search and visibility scope."`
}

type ListModelsResult struct {
	Models         []AgentModelSummary `json:"models"`
	Truncated      bool                `json:"truncated,omitempty"`
	DetailsOmitted bool                `json:"details_omitted,omitempty"`
	NextCursor     string              `json:"next_cursor,omitempty"`
}

type AgentGetModelRequest struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Ref       string `json:"ref" jsonschema:"Canonical model ref returned by list_models."`
}

type AgentModelDetail struct {
	AgentModelSummary
	AIContext  ossie.AIContext `json:"ai_context,omitempty"`
	Datasets   []string        `json:"datasets,omitempty"`
	Metrics    []string        `json:"metrics,omitempty"`
	Dimensions []string        `json:"dimensions,omitempty"`
}

type AgentGetModelResult struct {
	Model AgentModelDetail `json:"model"`
}

type ListMetricsRequest struct {
	ProjectID string   `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Models    []string `json:"models,omitempty" jsonschema:"Optional canonical model refs returned by list_models. Metrics from any selected model are returned; refs are deduplicated."`
	Search    []string `json:"search,omitempty"`
	Limit     *int     `json:"limit,omitempty" jsonschema:"Maximum metrics to return; defaults to 10 and cannot exceed 50."`
	Cursor    string   `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by an earlier request with the same models, search, and visibility scope."`
}

type AgentMetricSummary struct {
	Ref                      string                         `json:"ref"`
	Name                     string                         `json:"name"`
	Model                    string                         `json:"model"`
	ModelRef                 string                         `json:"model_ref"`
	DataType                 ossie.DataType                 `json:"data_type,omitempty"`
	Aliases                  []string                       `json:"aliases,omitempty"`
	SemanticKinds            []MetricSemanticConstraintKind `json:"semantic_kinds,omitempty"`
	SemanticConstraints      []MetricSemanticConstraint     `json:"semantic_constraints,omitempty"`
	Description              string                         `json:"description,omitempty"`
	Dimensions               []string                       `json:"dimensions,omitempty"`
	DefinitionEquivalentRefs []string                       `json:"definition_equivalent_refs,omitempty"`
	Governance               AssetGovernanceEvidence        `json:"governance"`
}

type ListMetricsResult struct {
	Metrics              []AgentMetricSummary       `json:"metrics"`
	SelectionRequired    bool                       `json:"selection_required,omitempty"`
	SelectionDifferences []AgentMetricSelectionAxis `json:"selection_differences,omitempty"`
	AmbiguousNames       []AgentMetricNameAmbiguity `json:"ambiguous_names,omitempty"`
	Truncated            bool                       `json:"truncated,omitempty"`
	DetailsOmitted       bool                       `json:"details_omitted,omitempty"`
	NextCursor           string                     `json:"next_cursor,omitempty"`
}

type AgentMetricSelectionAxis string

const (
	AgentMetricSelectionName                 AgentMetricSelectionAxis = "name"
	AgentMetricSelectionModel                AgentMetricSelectionAxis = "model_ref"
	AgentMetricSelectionDataType             AgentMetricSelectionAxis = "data_type"
	AgentMetricSelectionAliases              AgentMetricSelectionAxis = "authored_aliases"
	AgentMetricSelectionSemanticConstraints  AgentMetricSelectionAxis = "semantic_constraints"
	AgentMetricSelectionCompatibleDimensions AgentMetricSelectionAxis = "compatible_dimensions"
)

// AgentMetricNameAmbiguity records an exact metric-name collision across
// semantic models. Retrieval ordering is never permission to pick one ref.
type AgentMetricNameAmbiguity struct {
	Name string   `json:"name"`
	Refs []string `json:"refs"`
}

type AgentGetMetricRequest struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Ref       string `json:"ref" jsonschema:"Canonical metric ref returned by list_metrics."`
}

type AgentMetricDetail struct {
	AgentMetricSummary
	AIContext ossie.AIContext `json:"ai_context,omitempty"`
}

type AgentGetMetricResult struct {
	Metric AgentMetricDetail `json:"metric"`
}

type GetDimensionsRequest struct {
	ProjectID string   `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Model     string   `json:"model,omitempty" jsonschema:"Canonical model ref for metric-free dimension discovery; mutually exclusive with metrics."`
	Metrics   []string `json:"metrics,omitempty" jsonschema:"Canonical metric refs; only dimensions compatible with every selected metric are returned. Mutually exclusive with model."`
	Search    []string `json:"search,omitempty" jsonschema:"Optional terms applied after metric compatibility or model scope has been resolved."`
	Limit     *int     `json:"limit,omitempty" jsonschema:"Maximum dimensions to return; defaults to 20 and cannot exceed 100."`
	Cursor    string   `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by an earlier request with the same model, metrics, and search."`
}

type AgentDimensionSummary struct {
	Ref                string                   `json:"ref"`
	Name               string                   `json:"name"`
	Model              string                   `json:"model"`
	ModelRef           string                   `json:"model_ref"`
	Dataset            string                   `json:"dataset"`
	Type               AgentGroupByType         `json:"type"`
	CanonicalGroupings []AgentCanonicalGrouping `json:"canonical_groupings,omitempty"`
	Governance         AssetGovernanceEvidence  `json:"governance"`
}

// AgentCanonicalGrouping makes custom-calendar lowering explicit at discovery
// time: the Agent groups by the canonical base time dimension and Metis owns
// lowering that semantic grain to the physical bucket field.
type AgentCanonicalGrouping struct {
	Grain          query.TimeGrain   `json:"grain"`
	GroupBy        AgentGroupByParam `json:"group_by"`
	PhysicalBucket string            `json:"physical_bucket"`
}

type AgentDimensionSemanticEvidence struct {
	Effect string            `json:"effect"`
	Grains []query.TimeGrain `json:"grains,omitempty"`
}

const AgentSemanticEffectPreservesEmptyPeriods = "preserves_empty_periods"

type GetDimensionsResult struct {
	Dimensions []AgentDimensionSummary `json:"dimensions"`
	Truncated  bool                    `json:"truncated,omitempty"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type AgentGetDimensionRequest struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Ref       string `json:"ref" jsonschema:"Copy the exact canonical dimension ref returned by get_dimensions; do not construct it from name, model, dataset, or project."`
}

type AgentDimensionDetail struct {
	AgentDimensionSummary
	DataType         ossie.DataType                   `json:"data_type,omitempty"`
	Label            string                           `json:"label,omitempty"`
	PrimaryKey       bool                             `json:"primary_key,omitempty"`
	Description      string                           `json:"description,omitempty"`
	AIContext        ossie.AIContext                  `json:"ai_context,omitempty"`
	ValidGrains      []query.TimeGrain                `json:"valid_grains,omitempty"`
	SemanticEvidence []AgentDimensionSemanticEvidence `json:"semantic_evidence,omitempty"`
}

type AgentGetDimensionResult struct {
	Dimension AgentDimensionDetail `json:"dimension"`
}

type AgentGetRelationshipsRequest struct {
	ProjectID string   `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Model     string   `json:"model" jsonschema:"Canonical model ref returned by list_models."`
	Datasets  []string `json:"datasets,omitempty" jsonschema:"Optional dataset names returned by get_dimensions; relationships touching any are returned."`
	Search    []string `json:"search,omitempty" jsonschema:"Optional terms matched against relationship identity, endpoints, join columns, authored context, and temporal evidence."`
}

type AgentGetRelationshipsResult struct {
	Relationships []AgentRelationshipSummary `json:"relationships"`
}

type AgentRelationshipSummary struct {
	Ref              string                              `json:"ref"`
	ModelRef         string                              `json:"model_ref"`
	Name             string                              `json:"name"`
	From             string                              `json:"from"`
	To               string                              `json:"to"`
	FromColumns      []string                            `json:"from_columns,omitempty"`
	ToColumns        []string                            `json:"to_columns,omitempty"`
	AIContext        ossie.AIContext                     `json:"ai_context,omitempty"`
	SemanticEvidence []AgentRelationshipSemanticEvidence `json:"semantic_evidence,omitempty"`
	Governance       AssetGovernanceEvidence             `json:"governance"`
}

type AgentRelationshipSemanticEvidence struct {
	Effect            string `json:"effect"`
	FromTimeDimension string `json:"from_time_dimension,omitempty"`
	ToValidFrom       string `json:"to_valid_from,omitempty"`
	ToValidTo         string `json:"to_valid_to,omitempty"`
	Cardinality       string `json:"cardinality,omitempty"`
}

const AgentSemanticEffectPointInTime = "point_in_time"

type AgentGroupByType string

const (
	AgentGroupByDimension     AgentGroupByType = "dimension"
	AgentGroupByTimeDimension AgentGroupByType = "time_dimension"
)

type AgentGroupByParam struct {
	Name  string           `json:"name" jsonschema:"Canonical dimension ref returned by get_dimensions. For custom grains copy canonical_groupings[].group_by.name; never use physical_bucket."`
	Type  AgentGroupByType `json:"type" jsonschema:"Use the exact type returned by get_dimensions or canonical_groupings[].group_by.type: dimension or time_dimension."`
	Grain *query.TimeGrain `json:"grain,omitempty" jsonschema:"For time_dimension only. For custom calendars copy canonical_groupings[].group_by.grain; otherwise use get_dimension valid_grains."`
}

type AgentOrderByParam struct {
	Name       string `json:"name" jsonschema:"Canonical output metric or group_by dimension ref. The array order is sort precedence."`
	Descending bool   `json:"descending,omitempty" jsonschema:"True for descending; omit or false for ascending."`
}

// AgentCompileRequest keeps semantic intent compact. Dialect selects the
// compile-only physical SQL output and never describes runtime placement.
type AgentCompileRequest struct {
	ProjectID               string              `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Dialect                 sql.SQLDialect      `json:"dialect" jsonschema:"Required compile-only SQL dialect. MCP exposes the supported values as an enum."`
	Model                   string              `json:"model,omitempty" jsonschema:"Canonical model ref for metric-free queries; otherwise inferred from semantic refs."`
	OutputMetrics           []string            `json:"output_metrics,omitempty" jsonschema:"Canonical metric refs that must be visible result columns. Do not include metrics used only by filters."`
	GroupBy                 []AgentGroupByParam `json:"group_by,omitempty" jsonschema:"Visible dimension columns using refs and types returned by get_dimensions."`
	Filters                 []query.Filter      `json:"filters,omitempty" jsonschema:"Typed semantic predicates. Multiple filters are combined with AND."`
	OrderBy                 []AgentOrderByParam `json:"order_by,omitempty" jsonschema:"Sort keys in precedence order; use only selected output metrics or group_by dimensions."`
	Limit                   *int                `json:"limit,omitempty" jsonschema:"Maximum result rows."`
	projectContextInherited bool
}

// AgentQueryMetricsRequest mirrors only semantic intent from compile_sql. It
// has no dialect or physical placement fields because a project's configured
// DataSource type selects the Backend and its Renderer.
type AgentQueryMetricsRequest struct {
	ProjectID               string              `json:"project_id,omitempty" jsonschema:"Explicit project override only. Omit when an active project is configured; never use a model name."`
	Model                   string              `json:"model,omitempty" jsonschema:"Canonical model ref is optional when metric refs identify one model."`
	OutputMetrics           []string            `json:"output_metrics,omitempty" jsonschema:"At least one canonical metric ref is required."`
	GroupBy                 []AgentGroupByParam `json:"group_by,omitempty" jsonschema:"Visible dimension columns using refs and types returned by get_dimensions."`
	Filters                 []query.Filter      `json:"filters,omitempty" jsonschema:"Typed semantic predicates. Multiple filters are combined with AND."`
	OrderBy                 []AgentOrderByParam `json:"order_by,omitempty" jsonschema:"Sort keys in precedence order; use only selected output metrics or group_by dimensions."`
	Limit                   *int                `json:"limit,omitempty" jsonschema:"Maximum result rows, tightened by deployment policy."`
	projectContextInherited bool
}

func (r *AgentQueryMetricsRequest) MarkProjectContextInherited(inherited bool) {
	if r != nil {
		r.projectContextInherited = inherited
	}
}

func (r *AgentCompileRequest) MarkProjectContextInherited(inherited bool) {
	if r != nil {
		r.projectContextInherited = inherited
	}
}

func (s *AgentSemanticService) semanticManifest() (*manifest.SemanticManifest, error) {
	if s == nil || s.discovery == nil || s.discovery.manifest == nil {
		return nil, fmt.Errorf("agent semantic service is not configured")
	}
	semanticManifest := s.discovery.manifest.Current()
	if semanticManifest == nil {
		return nil, fmt.Errorf("semantic manifest is not loaded")
	}
	return semanticManifest, nil
}

func (s *AgentSemanticService) ListProjects(ctx context.Context, req ListProjectsRequest) (*ListProjectsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	semanticManifest, err := s.semanticManifest()
	if err != nil {
		return nil, err
	}
	requested := normalizeAgentProjectIDs(req.ProjectIDs)
	projectNames := make([]string, 0, len(semanticManifest.Projects))
	for name := range semanticManifest.Projects {
		if len(requested) > 0 {
			if _, ok := requested[name]; !ok {
				continue
			}
		}
		if !s.canAccess(ctx, name, ProjectActionDiscover) {
			continue
		}
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)

	projects := make([]AgentProjectSummary, 0, len(projectNames))
	for _, projectName := range projectNames {
		project := semanticManifest.Projects[projectName]
		displayName := project.DisplayName
		if displayName == "" {
			displayName = projectName
		}
		var capabilities []AgentProjectCapability
		if s.canAccess(ctx, projectName, ProjectActionCompile) {
			capabilities = append(capabilities, AgentCapabilityCompileSQL)
		}
		if s.capabilities != nil {
			for _, capability := range s.capabilities.ProjectCapabilities(ctx, projectName) {
				action, ok := agentCapabilityAction(capability)
				if ok && s.canAccess(ctx, projectName, action) {
					capabilities = append(capabilities, capability)
				}
			}
		}
		capabilities = normalizeAgentProjectCapabilities(capabilities)
		projects = append(projects, AgentProjectSummary{ProjectID: projectName, Name: displayName, Description: project.Description, Capabilities: capabilities})
	}
	return &ListProjectsResult{Projects: projects}, nil
}

func agentCapabilityAction(capability AgentProjectCapability) (ProjectAction, bool) {
	switch capability {
	case AgentCapabilityCompileSQL:
		return ProjectActionCompile, true
	case AgentCapabilityQueryMetrics:
		return ProjectActionExecute, true
	default:
		return "", false
	}
}

func normalizeAgentProjectCapabilities(values []AgentProjectCapability) []AgentProjectCapability {
	seen := make(map[AgentProjectCapability]struct{}, len(values))
	for _, value := range values {
		switch value {
		case AgentCapabilityCompileSQL, AgentCapabilityQueryMetrics:
			seen[value] = struct{}{}
		}
	}
	capabilities := make([]AgentProjectCapability, 0, len(seen))
	for value := range seen {
		capabilities = append(capabilities, value)
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	return capabilities
}

func (s *AgentSemanticService) ListModels(ctx context.Context, req ListModelsRequest) (*ListModelsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	limit, err := agentListLimit(req.Limit, 10)
	if err != nil {
		return nil, err
	}
	if err := validateAgentSearchTerms(req.Search); err != nil {
		return nil, err
	}
	search := normalizeAgentSearchTerms(req.Search)
	cursorSearch := append([]string(nil), search...)
	sort.Strings(cursorSearch)
	discoveryIndex, err := s.discovery.discoveryIndex()
	if err != nil {
		return nil, err
	}
	projectID := s.resolvedProjectID(req.ProjectID)
	binding := s.discovery.cursorBinding(ctx, "list_models", projectID, struct {
		Search []string `json:"search,omitempty"`
	}{Search: cursorSearch})
	cursor, err := decodeDiscoveryCursor(req.Cursor, binding)
	if err != nil {
		return nil, err
	}
	projectDiscovery := discoveryIndex.projects[projectID]
	if projectDiscovery == nil {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": projectID}}
	}
	type rankedModel struct {
		name  string
		score int
	}
	candidates := projectDiscovery.modelSearch.candidates(search)
	ranked := make([]rankedModel, 0, min(len(candidates), limit))
	for _, ordinal := range candidates {
		name := projectDiscovery.models[ordinal].name
		model := project.Models[name]
		if model == nil || model.Model == nil {
			continue
		}
		if !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(name), model.Model.CustomExtensions) {
			continue
		}
		score, matched := s.agentVisibleModelSearchScore(ctx, projectID, search, name, model)
		if matched {
			ranked = append(ranked, rankedModel{name: name, score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].name < ranked[j].name
	})
	if cursor.Offset > len(ranked) {
		return nil, invalidDiscoveryCursor()
	}
	start := cursor.Offset
	end := min(start+limit, len(ranked))
	models := make([]AgentModelSummary, 0, end-start)
	for _, candidate := range ranked[start:end] {
		models = append(models, agentModelSummary(candidate.name, project.Models[candidate.name]))
	}
	truncated := end < len(ranked)
	result := &ListModelsResult{Models: models, Truncated: truncated}
	if truncated {
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	detailsOmitted := truncated || encodedAgentListExceedsBudget(result)
	if detailsOmitted {
		for i := range models {
			models[i].Description = ""
		}
		for len(models) > 1 {
			result.Models = models
			if start+len(models) < len(ranked) {
				result.NextCursor = encodeDiscoveryCursor(binding, start+len(models), "")
			}
			if !encodedAgentListExceedsBudget(result) {
				break
			}
			models = models[:len(models)-1]
			result.Truncated = true
		}
	}
	result.Models = models
	if start+len(models) < len(ranked) {
		result.NextCursor = encodeDiscoveryCursor(binding, start+len(models), "")
		result.Truncated = true
	} else {
		result.NextCursor = ""
	}
	result.DetailsOmitted = detailsOmitted
	return result, nil
}

func (s *AgentSemanticService) GetModel(ctx context.Context, req AgentGetModelRequest) (*AgentGetModelResult, error) {
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	name, err := resolveAgentModelRef(project, req.Ref)
	if err != nil {
		return nil, err
	}
	model := project.Models[name]
	projectID := s.resolvedProjectID(req.ProjectID)
	if model == nil || model.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(name), model.Model.CustomExtensions) {
		return nil, modelNotFound(name)
	}
	detail := agentModelDetail(name, model)
	detail.Datasets = detail.Datasets[:0]
	detail.Metrics = detail.Metrics[:0]
	detail.Dimensions = detail.Dimensions[:0]
	for datasetName, dataset := range model.Datasets {
		if dataset != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(name, datasetName), dataset.CustomExtensions) {
			detail.Datasets = append(detail.Datasets, datasetName)
		}
	}
	for metricName, metric := range model.Metrics {
		if metric != nil && s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, name, metricName) {
			detail.Metrics = append(detail.Metrics, metricName)
		}
	}
	for qualified, handle := range model.Fields {
		if s.visibleDimension(ctx, projectID, name, qualified, model, handle, ProjectActionDiscover) {
			detail.Dimensions = append(detail.Dimensions, qualified)
		}
	}
	sort.Strings(detail.Datasets)
	sort.Strings(detail.Metrics)
	sort.Strings(detail.Dimensions)
	return &AgentGetModelResult{Model: detail}, nil
}

func (s *AgentSemanticService) ListMetrics(ctx context.Context, req ListMetricsRequest) (*ListMetricsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	limit, err := agentListLimit(req.Limit, 10)
	if err != nil {
		return nil, err
	}
	if err := validateAgentSearchTerms(req.Search); err != nil {
		return nil, err
	}
	search := normalizeAgentSearchTerms(req.Search)
	cursorSearch := append([]string(nil), search...)
	sort.Strings(cursorSearch)
	var modelNames []string
	if len(req.Models) > 0 {
		modelNames = make([]string, 0, len(req.Models))
		seen := make(map[string]struct{}, len(req.Models))
		for _, ref := range req.Models {
			modelName, err := resolveAgentModelRef(project, ref)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			modelNames = append(modelNames, modelName)
		}
	} else if len(search) == 0 {
		modelNames = make([]string, 0, len(project.Models))
		for name := range project.Models {
			modelNames = append(modelNames, name)
		}
	}
	sort.Strings(modelNames)
	projectID := s.resolvedProjectID(req.ProjectID)
	binding := s.discovery.cursorBinding(ctx, "list_metrics", projectID, struct {
		Models []string `json:"models,omitempty"`
		Search []string `json:"search,omitempty"`
	}{Models: modelNames, Search: cursorSearch})
	cursor, err := decodeDiscoveryCursor(req.Cursor, binding)
	if err != nil {
		return nil, err
	}
	var allowedModels map[string]struct{}
	if len(req.Models) > 0 {
		allowedModels = make(map[string]struct{}, len(modelNames))
		for _, modelName := range modelNames {
			model := project.Models[modelName]
			if model == nil || model.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(modelName), model.Model.CustomExtensions) {
				return nil, modelNotFound(modelName)
			}
			allowedModels[modelName] = struct{}{}
		}
	}
	discoveryIndex, err := s.discovery.discoveryIndex()
	if err != nil {
		return nil, err
	}
	projectDiscovery := discoveryIndex.projects[projectID]
	if projectDiscovery == nil {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": projectID}}
	}

	type rankedMetric struct {
		summary            AgentMetricSummary
		constraints        []MetricSemanticConstraint
		score              int
		matchedConstraints bool
	}
	candidates := projectDiscovery.metricSearch.candidates(search)
	ranked := make([]rankedMetric, 0, min(len(candidates), limit))
	modelVisibility := make(map[string]bool)
	for _, ordinal := range candidates {
		candidate := projectDiscovery.metrics[ordinal]
		modelName, name := candidate.model, candidate.name
		if allowedModels != nil {
			if _, ok := allowedModels[modelName]; !ok {
				continue
			}
		}
		model := project.Models[modelName]
		visibleModel, checked := modelVisibility[modelName]
		if !checked {
			visibleModel = model != nil && model.Model != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(modelName), model.Model.CustomExtensions)
			modelVisibility[modelName] = visibleModel
		}
		if !visibleModel {
			continue
		}
		metric := model.Metrics[name]
		if metric == nil || !s.discovery.indexedMetricVisible(ctx, projectID, model, candidate) {
			continue
		}
		summary, err := agentMetricSummary(modelName, metric)
		if err != nil {
			return nil, err
		}
		constraints := candidate.constraints
		constraintValues := metricConstraintSearchValues(constraints)
		score, matched := agentSearchScore(search,
			append([]string{name, summary.Ref}, summary.Aliases...),
			modelName, metric.Description, searchableAIContext(metric.AIContext), strings.Join(constraintValues, " "),
		)
		if !matched {
			continue
		}
		summary.SemanticKinds = metricConstraintKinds(constraints)
		_, matchedConstraints := agentSearchScore(search, constraintValues)
		ranked = append(ranked, rankedMetric{summary: summary, constraints: constraints, score: score, matchedConstraints: matchedConstraints})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].summary.Ref < ranked[j].summary.Ref
	})
	if len(search) == 0 && len(modelNames) > 1 {
		byModel := make(map[string][]rankedMetric, len(modelNames))
		for _, candidate := range ranked {
			byModel[candidate.summary.Model] = append(byModel[candidate.summary.Model], candidate)
		}
		fair := make([]rankedMetric, 0, len(ranked))
		for index := 0; len(fair) < len(ranked); index++ {
			for _, modelName := range modelNames {
				candidates := byModel[modelName]
				if index < len(candidates) {
					fair = append(fair, candidates[index])
				}
			}
		}
		ranked = fair
	}
	if cursor.Offset > len(ranked) {
		return nil, invalidDiscoveryCursor()
	}
	start := cursor.Offset
	end := min(start+limit, len(ranked))
	truncated := end < len(ranked)
	ranked = ranked[start:end]
	metrics := make([]AgentMetricSummary, len(ranked))
	for i := range ranked {
		metrics[i] = ranked[i].summary
		metrics[i].DefinitionEquivalentRefs, err = definitionEquivalentMetricRefs(project.Models[metrics[i].Model], metrics[i].Name)
		if err != nil {
			return nil, err
		}
		visibleEquivalentRefs := metrics[i].DefinitionEquivalentRefs[:0]
		equivalentPrefix := "metric:" + metrics[i].Model + "."
		for _, ref := range metrics[i].DefinitionEquivalentRefs {
			equivalentName := strings.TrimPrefix(ref, equivalentPrefix)
			candidate := project.Models[metrics[i].Model].Metrics[equivalentName]
			if strings.HasPrefix(ref, equivalentPrefix) && candidate != nil && s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, metrics[i].Model, equivalentName) {
				visibleEquivalentRefs = append(visibleEquivalentRefs, ref)
			}
		}
		metrics[i].DefinitionEquivalentRefs = visibleEquivalentRefs
		// Typed selection parameters remain compact only after retrieval has
		// narrowed the candidate set. Larger pages expose semantic_kinds and
		// searchable constraint evidence, then direct the Agent to narrow.
		if ((len(search) > 0 || len(req.Models) > 0) && len(ranked) <= 5) || ranked[i].matchedConstraints {
			metrics[i].SemanticConstraints = ranked[i].constraints
		}
	}
	// Dimension hints are authoritative only after the Agent has selected one
	// model. Unscoped search can match the same generic metric name across many
	// models; attaching every compatible dimension makes the response large and
	// encourages the Agent to combine refs from different semantic namespaces.
	modelScoped := len(req.Models) > 0 && len(modelNames) == 1
	if modelScoped && !truncated && len(metrics) <= 5 {
		for i := range metrics {
			compatibility, err := s.discovery.GetMetricsDimensions(ctx, GetMetricsDimensionsRequest{
				Project: req.ProjectID, Model: metrics[i].Model, Metrics: []string{metrics[i].Name},
			})
			if err != nil {
				return nil, err
			}
			for _, dimension := range compatibility.Dimensions {
				if dimension.Status == CompatibilityCompatible {
					metrics[i].Dimensions = append(metrics[i].Dimensions, "dimension:"+metrics[i].Model+"."+dimension.Qualified)
				}
			}
			sort.Strings(metrics[i].Dimensions)
		}
	}
	result := &ListMetricsResult{
		Metrics:   metrics,
		Truncated: truncated,
	}
	if truncated {
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	// Follow dbt MCP's progressive-disclosure shape: an unconstrained catalog
	// listing is an identity index, while a focused search keeps authored
	// details. This preserves open discovery without paying repeated context for
	// descriptions the caller has not narrowed to yet.
	broadListing := len(search) == 0 && len(req.Models) == 0
	detailsOmitted := broadListing || truncated || encodedAgentListExceedsBudget(result)
	if detailsOmitted {
		for i := range metrics {
			metrics[i].Description = ""
			metrics[i].Dimensions = nil
			if broadListing {
				metrics[i].SemanticConstraints = nil
			}
		}
		for len(metrics) > 1 {
			result.Metrics = metrics
			if start+len(metrics) < start+len(ranked) || truncated {
				result.NextCursor = encodeDiscoveryCursor(binding, start+len(metrics), "")
			}
			if !encodedAgentListExceedsBudget(result) {
				break
			}
			metrics = metrics[:len(metrics)-1]
			result.Truncated = true
		}
	}
	result.Metrics = metrics
	if start+len(metrics) < end || truncated {
		result.NextCursor = encodeDiscoveryCursor(binding, start+len(metrics), "")
		result.Truncated = true
	} else {
		result.NextCursor = ""
	}
	result.SelectionRequired = result.Truncated || len(metrics) > 1
	result.SelectionDifferences = agentMetricSelectionDifferences(metrics)
	result.AmbiguousNames = agentMetricNameAmbiguities(metrics)
	result.DetailsOmitted = detailsOmitted
	return result, nil
}

func agentMetricNameAmbiguities(metrics []AgentMetricSummary) []AgentMetricNameAmbiguity {
	byName := make(map[string][]string)
	for _, metric := range metrics {
		byName[metric.Name] = append(byName[metric.Name], metric.Ref)
	}
	names := make([]string, 0, len(byName))
	for name, refs := range byName {
		if len(refs) > 1 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	ambiguities := make([]AgentMetricNameAmbiguity, 0, len(names))
	for _, name := range names {
		refs := byName[name]
		sort.Strings(refs)
		ambiguities = append(ambiguities, AgentMetricNameAmbiguity{Name: name, Refs: refs})
	}
	return ambiguities
}

func definitionEquivalentMetricRefs(model *manifest.ModelIndex, metricName string) ([]string, error) {
	if model == nil || model.Model == nil || model.Metrics[metricName] == nil {
		return nil, nil
	}
	selected := model.Metrics[metricName]
	selectedConstraints, err := metricSemanticConstraints(selected)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for name, candidate := range model.Metrics {
		if name == metricName || candidate == nil || candidate.Datatype != selected.Datatype || !reflect.DeepEqual(candidate.Expression, selected.Expression) || !reflect.DeepEqual(candidate.CustomExtensions, selected.CustomExtensions) {
			continue
		}
		constraints, err := metricSemanticConstraints(candidate)
		if err != nil {
			return nil, err
		}
		if reflect.DeepEqual(constraints, selectedConstraints) {
			refs = append(refs, "metric:"+model.Model.Name+"."+name)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

func agentMetricSelectionDifferences(metrics []AgentMetricSummary) []AgentMetricSelectionAxis {
	if len(metrics) < 2 {
		return nil
	}
	first := metrics[0]
	var names, models, dataTypes, aliases, constraints, dimensions bool
	for _, metric := range metrics[1:] {
		names = names || metric.Name != first.Name
		models = models || metric.ModelRef != first.ModelRef
		dataTypes = dataTypes || metric.DataType != first.DataType
		aliases = aliases || !reflect.DeepEqual(metric.Aliases, first.Aliases)
		constraints = constraints || !reflect.DeepEqual(metric.SemanticKinds, first.SemanticKinds) || !reflect.DeepEqual(metric.SemanticConstraints, first.SemanticConstraints)
		dimensions = dimensions || !reflect.DeepEqual(metric.Dimensions, first.Dimensions)
	}
	axes := make([]AgentMetricSelectionAxis, 0, 6)
	for _, candidate := range []struct {
		different bool
		axis      AgentMetricSelectionAxis
	}{
		{names, AgentMetricSelectionName},
		{models, AgentMetricSelectionModel},
		{dataTypes, AgentMetricSelectionDataType},
		{aliases, AgentMetricSelectionAliases},
		{constraints, AgentMetricSelectionSemanticConstraints},
		{dimensions, AgentMetricSelectionCompatibleDimensions},
	} {
		if candidate.different {
			axes = append(axes, candidate.axis)
		}
	}
	return axes
}

func metricConstraintSearchValues(constraints []MetricSemanticConstraint) []string {
	values := make([]string, 0, len(constraints)*4)
	for _, constraint := range constraints {
		values = append(values, string(constraint.Kind))
		if conversion := constraint.Conversion; conversion != nil {
			values = append(values, conversion.BaseMetric, conversion.ConversionMetric, conversion.Calculation, conversion.Entity.BaseProperty, conversion.Entity.ConversionProperty)
			if conversion.Window != nil {
				values = append(values, conversion.Window.Unit)
			}
			for _, property := range conversion.ConstantProperties {
				values = append(values, property.BaseProperty, property.ConversionProperty)
			}
		}
		if cumulative := constraint.Cumulative; cumulative != nil {
			values = append(values, cumulative.BaseMetric, cumulative.TimeDimension, cumulative.WindowType, cumulative.Unit)
		}
		if definition := constraint.DefinitionFilter; definition != nil {
			for _, filter := range definition.Filters {
				values = append(values, filter.Field, string(filter.Operator))
			}
		}
		if fill := constraint.Fill; fill != nil {
			values = append(values, string(fill.Policy))
		}
		if offset := constraint.OffsetToGrain; offset != nil {
			values = append(values, offset.BaseMetric, offset.TimeDimension, offset.Grain)
		}
		if semi := constraint.SemiAdditive; semi != nil {
			values = append(values, semi.BaseMetric, semi.NonAdditiveDimension, semi.Selector, semi.TieBreakDimension, semi.NullPolicy, semi.RollupAggregation)
			values = append(values, semi.WindowGroupings...)
		}
		if binding := constraint.TimeBinding; binding != nil {
			values = append(values, binding.TimeDimension)
		}
		if offset := constraint.TimeOffset; offset != nil {
			values = append(values, offset.BaseMetric, offset.TimeDimension, offset.Unit)
		}
	}
	return values
}

func (s *AgentSemanticService) GetMetric(ctx context.Context, req AgentGetMetricRequest) (*AgentGetMetricResult, error) {
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	modelName, metricName, err := resolveAgentMetricRef(project, req.Ref)
	if err != nil {
		return nil, err
	}
	metric := project.Models[modelName].Metrics[metricName]
	projectID := s.resolvedProjectID(req.ProjectID)
	modelIndex := project.Models[modelName]
	if modelIndex == nil || modelIndex.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(modelName), modelIndex.Model.CustomExtensions) ||
		metric == nil || !s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, modelName, metricName) {
		return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": req.Ref}}
	}
	detail, err := agentMetricDetail(modelName, metric)
	if err != nil {
		return nil, err
	}
	detail.SemanticConstraints, err = metricSemanticConstraints(metric)
	if err != nil {
		return nil, err
	}
	detail.SemanticKinds = metricConstraintKinds(detail.SemanticConstraints)
	return &AgentGetMetricResult{Metric: detail}, nil
}

func metricConstraintKinds(constraints []MetricSemanticConstraint) []MetricSemanticConstraintKind {
	kinds := make([]MetricSemanticConstraintKind, len(constraints))
	for index := range constraints {
		kinds[index] = constraints[index].Kind
	}
	return kinds
}

func (s *AgentSemanticService) GetDimensions(ctx context.Context, req GetDimensionsRequest) (*GetDimensionsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := validateAgentSearchTerms(req.Search); err != nil {
		return nil, err
	}
	limit, err := agentDimensionListLimit(req.Limit)
	if err != nil {
		return nil, err
	}
	model, metricNames, err := resolveAgentDiscoveryModel(project, req.Model, req.Metrics)
	if err != nil {
		return nil, err
	}
	projectID := s.resolvedProjectID(req.ProjectID)
	modelIndex := project.Models[model]
	if modelIndex == nil || modelIndex.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(model), modelIndex.Model.CustomExtensions) {
		return nil, modelNotFound(model)
	}
	for _, metricName := range metricNames {
		metric := modelIndex.Metrics[metricName]
		if metric == nil || !s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, model, metricName) {
			return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": metricName}}
		}
	}
	normalizedMetrics := append([]string(nil), metricNames...)
	sort.Strings(normalizedMetrics)
	normalizedSearch := normalizeAgentSearchTerms(req.Search)
	sort.Strings(normalizedSearch)
	binding := s.discovery.cursorBinding(ctx, "get_dimensions", projectID, struct {
		Model   string   `json:"model"`
		Metrics []string `json:"metrics,omitempty"`
		Search  []string `json:"search,omitempty"`
	}{Model: model, Metrics: normalizedMetrics, Search: normalizedSearch})
	cursor, err := decodeDiscoveryCursor(req.Cursor, binding)
	if err != nil {
		return nil, err
	}
	if len(metricNames) == 0 {
		return modelScopedDimensions(project, model, req.Search, limit, cursor.Offset, binding, func(qualified string, handle *manifest.FieldHandle) bool {
			return s.visibleDimension(ctx, projectID, model, qualified, modelIndex, handle, ProjectActionDiscover)
		})
	}
	compatibility, err := s.discovery.GetMetricsDimensions(ctx, GetMetricsDimensionsRequest{
		Project: req.ProjectID,
		Model:   model,
		Metrics: metricNames,
	})
	if err != nil {
		return nil, err
	}
	search := normalizeAgentSearchTerms(req.Search)
	type rankedDimension struct {
		summary AgentDimensionSummary
		score   int
	}
	ranked := make([]rankedDimension, 0, len(compatibility.Dimensions))
	for _, dimension := range compatibility.Dimensions {
		if dimension.Status != CompatibilityCompatible || len(dimension.PerMetric) != len(metricNames) {
			continue
		}
		allCompatible := true
		for _, evidence := range dimension.PerMetric {
			if evidence.Status != CompatibilityCompatible {
				allCompatible = false
				break
			}
		}
		if !allCompatible {
			continue
		}
		handle := modelIndex.Fields[dimension.Qualified]
		if !s.visibleDimension(ctx, projectID, model, dimension.Qualified, modelIndex, handle, ProjectActionDiscover) {
			continue
		}
		description := ""
		dimensionType := AgentGroupByDimension
		if handle != nil && handle.Field != nil {
			description = handle.Field.Description
			if isAgentTimeDimension(handle.Field) {
				dimensionType = AgentGroupByTimeDimension
			}
		}
		summary, err := agentDimensionSummary(model, dimension.Qualified, modelIndex, handle)
		if err != nil {
			return nil, err
		}
		summary.Type = dimensionType
		secondary := []string{dimension.Dataset}
		if handle != nil && handle.Field != nil {
			secondary = append(secondary, handle.Field.Label, description, searchableAIContext(handle.Field.AIContext))
		}
		score, matched := agentSearchScore(search, agentDimensionSearchValues(summary), secondary...)
		if !matched {
			continue
		}
		ranked = append(ranked, rankedDimension{summary: summary, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].summary.Ref < ranked[j].summary.Ref
	})
	dimensions := make([]AgentDimensionSummary, len(ranked))
	for i := range ranked {
		dimensions[i] = ranked[i].summary
	}
	return paginateAgentDimensions(dimensions, limit, cursor.Offset, binding)
}

func (s *AgentSemanticService) GetDimension(ctx context.Context, req AgentGetDimensionRequest) (*AgentGetDimensionResult, error) {
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	modelName, qualified, handle, err := resolveAgentDimensionRef(project, req.Ref)
	if err != nil {
		return nil, err
	}
	model := project.Models[modelName]
	projectID := s.resolvedProjectID(req.ProjectID)
	if model == nil || model.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(modelName), model.Model.CustomExtensions) ||
		!s.visibleDimension(ctx, projectID, modelName, qualified, model, handle, ProjectActionDiscover) {
		return nil, agentDimensionNotFoundError(req.Ref)
	}
	detail, err := agentDimensionDetail(modelName, qualified, model, handle)
	if err != nil {
		return nil, err
	}
	return &AgentGetDimensionResult{Dimension: detail}, nil
}

func (s *AgentSemanticService) GetRelationships(ctx context.Context, req AgentGetRelationshipsRequest) (*AgentGetRelationshipsResult, error) {
	project, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := validateAgentSearchTerms(req.Search); err != nil {
		return nil, err
	}
	modelName, err := resolveAgentModelRef(project, req.Model)
	if err != nil {
		return nil, err
	}
	projectID := s.resolvedProjectID(req.ProjectID)
	modelIndex := project.Models[modelName]
	if modelIndex == nil || modelIndex.Model == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(modelName), modelIndex.Model.CustomExtensions) {
		return nil, modelNotFound(modelName)
	}
	datasets := make(map[string]struct{}, len(req.Datasets))
	for _, dataset := range req.Datasets {
		dataset = strings.TrimSpace(dataset)
		if dataset == "" {
			continue
		}
		asset := modelIndex.Datasets[dataset]
		if asset == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(modelName, dataset), asset.CustomExtensions) {
			return nil, &serrors.Error{Code: serrors.ErrFieldNotFound, Message: "dataset not found", Details: map[string]any{"model": modelName, "dataset": dataset}}
		}
		datasets[dataset] = struct{}{}
	}
	relationships, err := focusedAgentRelationships(modelIndex, datasets, normalizeAgentSearchTerms(req.Search), func(name string, relationship *ossie.Relationship) bool {
		if relationship == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(modelName, name), relationship.CustomExtensions) {
			return false
		}
		from, to := modelIndex.Datasets[relationship.From], modelIndex.Datasets[relationship.To]
		return from != nil && to != nil &&
			s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(modelName, relationship.From), from.CustomExtensions) &&
			s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(modelName, relationship.To), to.CustomExtensions)
	})
	if err != nil {
		return nil, err
	}
	return &AgentGetRelationshipsResult{Relationships: relationships}, nil
}

func isAgentTimeDimension(field *ossie.Field) bool {
	if field == nil || field.Dimension == nil {
		return false
	}
	if field.Dimension.IsTime != nil {
		return *field.Dimension.IsTime
	}
	switch field.Datatype {
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}

func supportsBuiltinTimeGrains(field *ossie.Field) bool {
	if field == nil {
		return false
	}
	switch field.Datatype {
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}

func agentTimeGrainMetadata(model *manifest.ModelIndex, dimension *manifest.FieldHandle) ([]query.TimeGrain, []AgentDimensionSemanticEvidence, error) {
	if dimension != nil && !supportsBuiltinTimeGrains(dimension.Field) {
		return nil, nil, nil
	}
	grains := []query.TimeGrain{
		query.TimeGrainYear,
		query.TimeGrainQuarter,
		query.TimeGrainMonth,
		query.TimeGrainWeek,
		query.TimeGrainDay,
		query.TimeGrainHour,
	}
	if model == nil || model.Model == nil || dimension == nil || dimension.Field == nil {
		return grains, nil, nil
	}
	calendar, ok, err := ossie.CustomCalendar(model.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve Agent time-grain metadata: %w", err)
	}
	if !ok || dimension.Dataset != calendar.Dataset || dimension.Field.Name != calendar.BaseTime {
		return grains, nil, nil
	}
	preservesEmptyPeriods := make([]query.TimeGrain, 0, len(calendar.Grains))
	for _, grain := range calendar.Grains {
		name := query.TimeGrain(grain.Name)
		grains = append(grains, name)
		if grain.DenseMapping {
			preservesEmptyPeriods = append(preservesEmptyPeriods, name)
		}
	}
	if len(preservesEmptyPeriods) == 0 {
		return grains, nil, nil
	}
	return grains, []AgentDimensionSemanticEvidence{{
		Effect: AgentSemanticEffectPreservesEmptyPeriods,
		Grains: preservesEmptyPeriods,
	}}, nil
}

func normalizeAgentSearchTerms(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	terms := make([]string, 0, len(values))
	for _, value := range values {
		term := normalizeAgentSearchText(value)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func validateAgentSearchTerms(values []string) error {
	actual := len(normalizeAgentSearchTerms(values))
	if actual <= maxAgentSearchTerms {
		return nil
	}
	return &serrors.Error{
		Code:    serrors.ErrInvalidQuery,
		Message: "search accepts at most 20 terms",
		Details: map[string]any{"maximum": maxAgentSearchTerms, "actual": actual},
	}
}

func normalizeAgentSearchText(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, strings.TrimSpace(value))), " ")
}

func matchesAnyAgentSearchTerm(terms []string, values ...string) bool {
	if len(terms) == 0 {
		return true
	}
	for _, value := range values {
		candidate := normalizeAgentSearchText(value)
		for _, term := range terms {
			if strings.Contains(candidate, term) {
				return true
			}
		}
	}
	return false
}

// agentSearchScore preserves substring discovery while placing the most
// specific deterministic matches first. Primary values are canonical names
// and refs; secondary values are descriptive discovery evidence. Matching all
// requested OR terms earns a bonus but partial matches remain discoverable.
func agentSearchScore(terms []string, primary []string, secondary ...string) (int, bool) {
	if len(terms) == 0 {
		return 0, true
	}
	normalize := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value = normalizeAgentSearchText(value); value != "" {
				result = append(result, value)
			}
		}
		return result
	}
	primary = normalize(primary)
	secondary = normalize(secondary)
	score, matched := 0, 0
	for _, term := range terms {
		best := 0
		for _, value := range primary {
			switch {
			case value == term:
				best = max(best, 80)
			case strings.Contains(value, term):
				best = max(best, 40)
			}
		}
		for _, value := range secondary {
			switch {
			case value == term:
				best = max(best, 20)
			case strings.Contains(value, term):
				best = max(best, 10)
			}
		}
		if best > 0 {
			matched++
			score += best
		}
	}
	if matched == 0 {
		return 0, false
	}
	if matched == len(terms) {
		score += 100
	}
	return score, true
}

func searchableAIContext(value ossie.AIContext) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

const agentListDetailBudgetBytes = 2048

func encodedAgentListExceedsBudget(value any) bool {
	encoded, err := json.Marshal(value)
	return err == nil && len(encoded) > agentListDetailBudgetBytes
}

func agentModelSearchScore(terms []string, name string, model *manifest.ModelIndex) (int, bool) {
	if model == nil || model.Model == nil {
		return agentSearchScore(terms, []string{name, "model:" + name})
	}
	// Score model identity first, then each contained semantic asset as one
	// coherent candidate. This prevents an all-term bonus assembled from
	// unrelated fields while still allowing any partial OR match to discover the
	// model. A metric such as revenue_at_fiscal_year_start can therefore outrank
	// a model whose name merely contains fiscal.
	best, matched := agentSearchScore(terms,
		[]string{name, "model:" + name},
		model.Model.Description, searchableAIContext(model.Model.AIContext),
	)
	scoreAsset := func(value string) {
		score, ok := agentSearchScore(terms, []string{value})
		if ok && (!matched || score > best) {
			best, matched = score, true
		}
	}
	for dataset := range model.Datasets {
		scoreAsset(dataset)
	}
	for metric := range model.Metrics {
		scoreAsset(metric)
	}
	for dimension, handles := range model.Dimensions {
		scoreAsset(dimension)
		for _, handle := range handles {
			if handle != nil {
				scoreAsset(handle.Dataset + "." + dimension)
			}
		}
	}
	return best, matched
}

func (s *AgentSemanticService) agentVisibleModelSearchScore(ctx context.Context, projectID string, terms []string, name string, model *manifest.ModelIndex) (int, bool) {
	if model == nil || model.Model == nil {
		return agentSearchScore(terms, []string{name, modelAssetRef(name)})
	}
	best, matched := agentSearchScore(terms, []string{name, modelAssetRef(name)}, model.Model.Description, searchableAIContext(model.Model.AIContext))
	scoreAsset := func(value string) {
		score, ok := agentSearchScore(terms, []string{value})
		if ok && (!matched || score > best) {
			best, matched = score, true
		}
	}
	for datasetName, dataset := range model.Datasets {
		if dataset != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(name, datasetName), dataset.CustomExtensions) {
			scoreAsset(datasetName)
		}
	}
	for metricName, metric := range model.Metrics {
		if metric != nil && s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, name, metricName) {
			scoreAsset(metricName)
		}
	}
	for qualified, handle := range model.Fields {
		if s.visibleDimension(ctx, projectID, name, qualified, model, handle, ProjectActionDiscover) {
			scoreAsset(qualified)
		}
	}
	return best, matched
}

func (s *AgentSemanticService) BuildCompileRequest(req AgentCompileRequest) (CompileRequest, error) {
	return s.BuildCompileRequestContext(context.Background(), req)
}

func (s *AgentSemanticService) BuildCompileRequestContext(ctx context.Context, req AgentCompileRequest) (CompileRequest, error) {
	return s.buildCompileRequestContext(ctx, req, ProjectActionCompile)
}

func (s *AgentSemanticService) buildCompileRequestContext(ctx context.Context, req AgentCompileRequest, action ProjectAction) (CompileRequest, error) {
	projectID, err := s.discovery.ResolveProject(req.ProjectID)
	if err != nil {
		return CompileRequest{}, err
	}
	req.ProjectID = projectID
	project, err := s.projectForAction(ctx, req.ProjectID, action)
	if err != nil {
		return CompileRequest{}, err
	}
	model, metricNames, err := resolveAgentCompileSelection(project, req)
	if err != nil {
		return CompileRequest{}, err
	}
	semanticQuery := query.SemanticQuery{
		Project: req.ProjectID,
		Model:   model,
		Limit:   req.Limit,
		Filters: append([]query.Filter(nil), req.Filters...),
	}
	for _, name := range metricNames {
		semanticQuery.Metrics = append(semanticQuery.Metrics, query.MetricRef{Name: name})
	}
	for _, group := range req.GroupBy {
		if group.Type != AgentGroupByDimension && group.Type != AgentGroupByTimeDimension {
			return CompileRequest{}, &serrors.Error{
				Code:    serrors.ErrInvalidQuery,
				Message: "group_by type must be dimension or time_dimension",
				Details: map[string]any{"type": group.Type},
			}
		}
		name, err := normalizeAgentSemanticRef(project, model, group.Name, "dimension")
		if err != nil {
			return CompileRequest{}, err
		}
		if err := validateAgentGroupBy(model, project.Models[model], name, group); err != nil {
			return CompileRequest{}, err
		}
		semanticQuery.Dimensions = append(semanticQuery.Dimensions, query.DimensionRef{Name: name, Grain: group.Grain})
	}
	for i := range semanticQuery.Filters {
		field, err := normalizeAgentFilterField(project, model, semanticQuery.Filters[i].Field)
		if err != nil {
			return CompileRequest{}, err
		}
		semanticQuery.Filters[i].Field = field
	}
	for _, order := range req.OrderBy {
		field, err := normalizeAgentOrderField(project, model, order.Name)
		if err != nil {
			return CompileRequest{}, err
		}
		direction := query.SortAsc
		if order.Descending {
			direction = query.SortDesc
		}
		semanticQuery.OrderBy = append(semanticQuery.OrderBy, query.OrderBy{Field: field, Direction: direction})
	}
	if s.discovery != nil {
		if err := s.discovery.authorizeSemanticQueryAssets(ctx, semanticQuery, action); err != nil {
			return CompileRequest{}, err
		}
	}
	return CompileRequest{Query: semanticQuery, Dialect: req.Dialect, ProjectContextInherited: req.projectContextInherited}, nil
}

// BuildQueryMetricsRequestContext applies the same canonical semantic-ref
// normalization as compile_sql while intentionally supplying no dialect.
func (s *AgentSemanticService) BuildQueryMetricsRequestContext(ctx context.Context, req AgentQueryMetricsRequest) (QueryMetricsRequest, error) {
	compiled, err := s.buildCompileRequestContext(ctx, AgentCompileRequest{
		ProjectID:               req.ProjectID,
		Model:                   req.Model,
		OutputMetrics:           req.OutputMetrics,
		GroupBy:                 req.GroupBy,
		Filters:                 req.Filters,
		OrderBy:                 req.OrderBy,
		Limit:                   req.Limit,
		projectContextInherited: req.projectContextInherited,
	}, ProjectActionExecute)
	if err != nil {
		return QueryMetricsRequest{}, err
	}
	return QueryMetricsRequest{Query: compiled.Query, ProjectContextInherited: compiled.ProjectContextInherited}, nil
}

func agentModelSummary(name string, model *manifest.ModelIndex) AgentModelSummary {
	summary := AgentModelSummary{Ref: "model:" + name, Name: name}
	if model != nil && model.Model != nil {
		summary.Description = model.Model.Description
		summary.Governance, _ = governanceEvidence(model.Model.CustomExtensions)
	}
	return summary
}

func agentModelDetail(name string, model *manifest.ModelIndex) AgentModelDetail {
	detail := AgentModelDetail{AgentModelSummary: agentModelSummary(name, model)}
	if model != nil && model.Model != nil {
		detail.AIContext = model.Model.AIContext
		for dataset := range model.Datasets {
			detail.Datasets = append(detail.Datasets, dataset)
		}
		for metric := range model.Metrics {
			detail.Metrics = append(detail.Metrics, metric)
		}
		for qualified, handle := range model.Fields {
			if handle != nil && handle.Field != nil && handle.Field.Dimension != nil {
				detail.Dimensions = append(detail.Dimensions, qualified)
			}
		}
		sort.Strings(detail.Datasets)
		sort.Strings(detail.Metrics)
		sort.Strings(detail.Dimensions)
	}
	return detail
}

func agentMetricSummary(modelName string, metric *ossie.Metric) (AgentMetricSummary, error) {
	summary := AgentMetricSummary{Ref: "metric:" + modelName + ".", Model: modelName, ModelRef: "model:" + modelName}
	if metric != nil {
		summary.Ref += metric.Name
		summary.Name = metric.Name
		summary.DataType = metric.Datatype
		summary.Description = metric.Description
		summary.Governance, _ = governanceEvidence(metric.CustomExtensions)
		discovery, ok, err := ossie.AgentDiscoverySpec(metric)
		if err != nil {
			return AgentMetricSummary{}, &serrors.Error{
				Code:    serrors.ErrInvalidModel,
				Message: "invalid authored metric aliases",
				Details: map[string]any{"metric": metric.Name, "cause": err.Error()},
			}
		}
		if ok {
			summary.Aliases = append([]string(nil), discovery.Aliases...)
		}
	}
	return summary, nil
}

func agentMetricDetail(modelName string, metric *ossie.Metric) (AgentMetricDetail, error) {
	summary, err := agentMetricSummary(modelName, metric)
	if err != nil {
		return AgentMetricDetail{}, err
	}
	detail := AgentMetricDetail{AgentMetricSummary: summary}
	if metric != nil {
		detail.AIContext = metric.AIContext
	}
	return detail, nil
}

func agentListLimit(raw *int, fallback int) (int, error) {
	if raw == nil {
		return fallback, nil
	}
	if *raw < 1 || *raw > 50 {
		return 0, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "list limit must be between 1 and 50", Details: map[string]any{"limit": *raw}}
	}
	return *raw, nil
}

func agentDimensionListLimit(raw *int) (int, error) {
	if raw == nil {
		return 20, nil
	}
	if *raw < 1 || *raw > 100 {
		return 0, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "dimension limit must be between 1 and 100", Details: map[string]any{"limit": *raw}}
	}
	return *raw, nil
}

func resolveAgentDiscoveryModel(project *manifest.ProjectIndex, rawModel string, metrics []string) (string, []string, error) {
	if strings.TrimSpace(rawModel) != "" && len(metrics) > 0 {
		return "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimensions accepts exactly one semantic anchor", Details: map[string]any{"anchors": []string{"model", "metrics"}}}
	}
	selectedModel := ""
	if strings.TrimSpace(rawModel) != "" {
		model, err := resolveAgentModelRef(project, rawModel)
		if err != nil {
			return "", nil, err
		}
		selectedModel = model
	}
	if len(metrics) == 0 {
		if selectedModel == "" {
			return "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "get_dimensions requires model when metrics are omitted", Details: map[string]any{"repair": "use a model ref returned by list_models"}}
		}
		return selectedModel, nil, nil
	}
	metricModel, metricNames, err := resolveAgentMetricRefs(project, metrics)
	if err != nil {
		return "", nil, err
	}
	if selectedModel != "" && selectedModel != metricModel {
		return "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "model does not match selected metrics", Details: map[string]any{"model": selectedModel, "metric_model": metricModel}}
	}
	return metricModel, metricNames, nil
}

func modelScopedDimensions(project *manifest.ProjectIndex, modelName string, searchValues []string, limit, offset int, binding discoveryCursorBinding, visible ...func(string, *manifest.FieldHandle) bool) (*GetDimensionsResult, error) {
	model := project.Models[modelName]
	if model == nil || model.Model == nil {
		return nil, &serrors.Error{Code: serrors.ErrModelNotFound, Message: "model not found", Details: map[string]any{"model": modelName}}
	}
	search := normalizeAgentSearchTerms(searchValues)
	type rankedDimension struct {
		summary AgentDimensionSummary
		score   int
	}
	ranked := make([]rankedDimension, 0, len(model.Fields))
	fieldNames := make([]string, 0, len(model.Fields))
	for name := range model.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	for _, qualified := range fieldNames {
		handle := model.Fields[qualified]
		if handle == nil || handle.Field == nil || handle.Field.Dimension == nil {
			continue
		}
		if len(visible) > 0 && visible[0] != nil && !visible[0](qualified, handle) {
			continue
		}
		field := handle.Field
		dimensionType := AgentGroupByDimension
		if isAgentTimeDimension(field) {
			dimensionType = AgentGroupByTimeDimension
		}
		summary, err := agentDimensionSummary(modelName, qualified, model, handle)
		if err != nil {
			return nil, err
		}
		summary.Type = dimensionType
		score, matched := agentSearchScore(search, agentDimensionSearchValues(summary), handle.Dataset, field.Label, field.Description, searchableAIContext(field.AIContext))
		if !matched {
			continue
		}
		ranked = append(ranked, rankedDimension{summary: summary, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].summary.Ref < ranked[j].summary.Ref
	})
	dimensions := make([]AgentDimensionSummary, len(ranked))
	for i := range ranked {
		dimensions[i] = ranked[i].summary
	}
	return paginateAgentDimensions(dimensions, limit, offset, binding)
}

func paginateAgentDimensions(dimensions []AgentDimensionSummary, limit, offset int, binding discoveryCursorBinding) (*GetDimensionsResult, error) {
	if offset < 0 || offset > len(dimensions) {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "dimension cursor is outside the current result set"}
	}
	end := offset + limit
	if end > len(dimensions) {
		end = len(dimensions)
	}
	result := &GetDimensionsResult{Dimensions: append([]AgentDimensionSummary(nil), dimensions[offset:end]...)}
	if end < len(dimensions) {
		result.Truncated = true
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	return result, nil
}

func focusedAgentRelationships(model *manifest.ModelIndex, datasets map[string]struct{}, search []string, visible ...func(string, *ossie.Relationship) bool) ([]AgentRelationshipSummary, error) {
	names := make([]string, 0, len(model.Relationships))
	for name := range model.Relationships {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]AgentRelationshipSummary, 0, len(names))
	for _, name := range names {
		relationship := model.Relationships[name]
		if relationship == nil {
			continue
		}
		if len(visible) > 0 && visible[0] != nil && !visible[0](name, relationship) {
			continue
		}
		_, fromSelected := datasets[relationship.From]
		_, toSelected := datasets[relationship.To]
		if len(datasets) > 0 && !fromSelected && !toSelected {
			continue
		}
		if !matchesAnyAgentSearchTerm(search, relationship.Name, relationship.From, relationship.To, searchableAIContext(relationship.AIContext)) {
			continue
		}
		summary := AgentRelationshipSummary{
			Ref: relationshipAssetRef(model.Model.Name, relationship.Name), ModelRef: modelAssetRef(model.Model.Name),
			Name: relationship.Name, From: relationship.From, To: relationship.To,
			FromColumns: append([]string(nil), relationship.FromColumns...), ToColumns: append([]string(nil), relationship.ToColumns...),
			AIContext: relationship.AIContext,
		}
		governance, err := governanceEvidence(relationship.CustomExtensions)
		if err != nil {
			return nil, err
		}
		summary.Governance = governance
		temporal, ok, err := ossie.TemporalRelationship(relationship)
		if err != nil {
			return nil, err
		}
		if ok {
			summary.SemanticEvidence = []AgentRelationshipSemanticEvidence{{
				Effect: AgentSemanticEffectPointInTime, FromTimeDimension: temporal.FromTimeDimension,
				ToValidFrom: temporal.ToValidFrom, ToValidTo: temporal.ToValidTo, Cardinality: temporal.Cardinality,
			}}
		}
		out = append(out, summary)
	}
	return out, nil
}

func fieldIsPrimaryKey(model *manifest.ModelIndex, handle *manifest.FieldHandle) bool {
	if model == nil || handle == nil || handle.Field == nil || model.Datasets[handle.Dataset] == nil {
		return false
	}
	for _, name := range model.Datasets[handle.Dataset].PrimaryKey {
		if name == handle.Field.Name {
			return true
		}
	}
	return false
}

func (s *AgentSemanticService) project(ctx context.Context, name string) (*manifest.ProjectIndex, error) {
	return s.projectForAction(ctx, name, ProjectActionDiscover)
}

func (s *AgentSemanticService) resolvedProjectID(name string) string {
	if s == nil || s.discovery == nil {
		return ""
	}
	resolved, err := s.discovery.ResolveProject(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(resolved)
}

func (s *AgentSemanticService) visibleDimension(ctx context.Context, projectID, modelName, _ string, model *manifest.ModelIndex, handle *manifest.FieldHandle, action ProjectAction) bool {
	if s == nil || s.discovery == nil || model == nil || handle == nil || handle.Field == nil || handle.Field.Dimension == nil {
		return false
	}
	dataset := model.Datasets[handle.Dataset]
	if dataset == nil || !s.discovery.canAccessAsset(ctx, projectID, action, AssetDataset, datasetAssetRef(modelName, handle.Dataset), dataset.CustomExtensions) {
		return false
	}
	return s.discovery.canAccessAsset(ctx, projectID, action, AssetDimension, dimensionAssetRef(modelName, handle.Dataset, handle.Field.Name), handle.Field.CustomExtensions)
}

func (s *AgentSemanticService) projectForAction(ctx context.Context, name string, action ProjectAction) (*manifest.ProjectIndex, error) {
	if s == nil || s.discovery == nil {
		return nil, fmt.Errorf("agent semantic service is not configured")
	}
	resolved, err := s.discovery.ResolveProject(name)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(resolved)
	if name == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	if err := authorizeProject(ctx, s.discovery.authorizer, s.discovery.authorizationObserver, name, action); err != nil {
		return nil, err
	}
	semanticManifest, err := s.semanticManifest()
	if err != nil {
		return nil, err
	}
	project, err := semanticManifest.Project(name)
	if err != nil {
		return nil, err
	}
	return project, nil
}

func (s *AgentSemanticService) canAccess(ctx context.Context, projectID string, action ProjectAction) bool {
	return s != nil && s.discovery != nil && s.discovery.CanAccessProject(ctx, projectID, action)
}

func normalizeAgentProjectIDs(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func resolveAgentCompileSelection(project *manifest.ProjectIndex, req AgentCompileRequest) (string, []string, error) {
	selectedModel := ""
	selectModel := func(candidate string) error {
		if candidate == "" {
			return nil
		}
		if selectedModel != "" && selectedModel != candidate {
			return &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "selected semantic refs must belong to one model", Details: map[string]any{"models": []string{selectedModel, candidate}}}
		}
		selectedModel = candidate
		return nil
	}
	if strings.TrimSpace(req.Model) != "" {
		model, err := resolveAgentModelRef(project, req.Model)
		if err != nil {
			return "", nil, err
		}
		if err := selectModel(model); err != nil {
			return "", nil, err
		}
	}
	metricNames := []string(nil)
	if len(req.OutputMetrics) > 0 {
		model, names, err := resolveAgentMetricRefs(project, req.OutputMetrics)
		if err != nil {
			return "", nil, err
		}
		if err := selectModel(model); err != nil {
			return "", nil, err
		}
		metricNames = names
	}
	refs := make([]string, 0, len(req.GroupBy)+len(req.Filters)+len(req.OrderBy))
	for _, group := range req.GroupBy {
		refs = append(refs, group.Name)
	}
	for _, filter := range req.Filters {
		refs = append(refs, filter.Field)
	}
	for _, order := range req.OrderBy {
		refs = append(refs, order.Name)
	}
	for _, ref := range refs {
		model, ok, err := modelFromCanonicalAgentRef(project, ref)
		if err != nil {
			return "", nil, err
		}
		if ok {
			if err := selectModel(model); err != nil {
				return "", nil, err
			}
		}
	}
	if selectedModel == "" {
		return "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "compile requires model when output_metrics are empty and semantic refs are unqualified", Details: map[string]any{"repair": "use model and dimension refs returned by list_models and get_dimensions"}}
	}
	return selectedModel, metricNames, nil
}

func resolveAgentModelRef(project *manifest.ProjectIndex, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if strings.HasPrefix(name, "model:") {
		name = strings.TrimPrefix(name, "model:")
	}
	if name == "" || project.Models[name] == nil {
		return "", &serrors.Error{Code: serrors.ErrModelNotFound, Message: "model not found", Details: map[string]any{"model": raw}}
	}
	return name, nil
}

func modelFromCanonicalAgentRef(project *manifest.ProjectIndex, raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{"metric:", "dimension:"} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		model, _, ok := splitAgentRef(project, strings.TrimPrefix(raw, prefix))
		if !ok {
			return "", false, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "semantic reference is invalid", Details: map[string]any{"ref": raw}}
		}
		return model, true, nil
	}
	return "", false, nil
}

func resolveAgentMetricRefs(project *manifest.ProjectIndex, refs []string) (string, []string, error) {
	if len(refs) == 0 {
		return "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "at least one metric is required"}
	}
	seen := map[string]struct{}{}
	metricNames := make([]string, 0, len(refs))
	selectedModel := ""
	for _, raw := range refs {
		if !strings.HasPrefix(strings.TrimSpace(raw), "metric:") {
			return "", nil, &serrors.Error{
				Code:    serrors.ErrInvalidQuery,
				Message: "selected metrics must use canonical metric refs",
				Details: map[string]any{"metric": raw, "repair": "use the exact metric ref returned by list_metrics"},
			}
		}
		model, name, err := resolveAgentMetricRef(project, raw)
		if err != nil {
			return "", nil, err
		}
		if selectedModel == "" {
			selectedModel = model
		} else if selectedModel != model {
			return "", nil, &serrors.Error{
				Code:    serrors.ErrInvalidQuery,
				Message: "selected metrics must belong to one semantic model in v1",
				Details: map[string]any{"models": []string{selectedModel, model}},
			}
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		metricNames = append(metricNames, name)
	}
	return selectedModel, metricNames, nil
}

func resolveAgentMetricRef(project *manifest.ProjectIndex, raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "metric reference is empty"}
	}
	if strings.HasPrefix(raw, "metric:") {
		model, name, ok := splitAgentRef(project, strings.TrimPrefix(raw, "metric:"))
		if !ok {
			return "", "", &serrors.Error{
				Code:    serrors.ErrMetricNotFound,
				Message: "metric not found",
				Details: map[string]any{"metric": raw},
			}
		}
		if _, ok := project.Models[model].Metrics[name]; !ok {
			return "", "", &serrors.Error{
				Code:    serrors.ErrMetricNotFound,
				Message: "metric not found",
				Details: map[string]any{"metric": raw},
			}
		}
		return model, name, nil
	}
	matches := make([]string, 0, 1)
	for model, index := range project.Models {
		if _, ok := index.Metrics[raw]; ok {
			matches = append(matches, model)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", "", &serrors.Error{
			Code:    serrors.ErrMetricNotFound,
			Message: "metric not found",
			Details: map[string]any{"metric": raw},
		}
	case 1:
		return matches[0], raw, nil
	default:
		return "", "", &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: "metric name is ambiguous across models; use the ref returned by list_metrics",
			Details: map[string]any{"metric": raw, "models": matches},
		}
	}
}

func resolveAgentDimensionRef(project *manifest.ProjectIndex, raw string) (string, string, *manifest.FieldHandle, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "dimension reference is empty"}
	}
	body := raw
	if strings.HasPrefix(body, "dimension:") {
		body = strings.TrimPrefix(body, "dimension:")
	}
	model, qualified, ok := splitAgentRef(project, body)
	if !ok {
		return "", "", nil, agentDimensionNotFoundError(raw)
	}
	handle := project.Models[model].Fields[qualified]
	if handle == nil || handle.Field == nil || handle.Field.Dimension == nil {
		return "", "", nil, agentDimensionNotFoundError(raw)
	}
	return model, qualified, handle, nil
}

func agentDimensionNotFoundError(raw string) error {
	return &serrors.Error{
		Code: serrors.ErrDimensionNotFound, Message: "dimension not found",
		Details: map[string]any{"dimension": raw, "repair": "copy the exact ref returned by get_dimensions; do not construct a ref"},
	}
}

func agentDimensionDetail(modelName, qualified string, model *manifest.ModelIndex, handle *manifest.FieldHandle) (AgentDimensionDetail, error) {
	field := handle.Field
	dimensionType := AgentGroupByDimension
	var grains []query.TimeGrain
	var evidence []AgentDimensionSemanticEvidence
	var err error
	if isAgentTimeDimension(field) {
		dimensionType = AgentGroupByTimeDimension
		grains, evidence, err = agentTimeGrainMetadata(model, handle)
		if err != nil {
			return AgentDimensionDetail{}, err
		}
	}
	summary, err := agentDimensionSummary(modelName, qualified, model, handle)
	if err != nil {
		return AgentDimensionDetail{}, err
	}
	summary.Type = dimensionType
	return AgentDimensionDetail{
		AgentDimensionSummary: summary,
		DataType:              field.Datatype, Label: field.Label, PrimaryKey: fieldIsPrimaryKey(model, handle),
		Description: field.Description, AIContext: field.AIContext, ValidGrains: grains, SemanticEvidence: evidence,
	}, nil
}

func agentDimensionSummary(modelName, qualified string, model *manifest.ModelIndex, handle *manifest.FieldHandle) (AgentDimensionSummary, error) {
	dimensionType := AgentGroupByDimension
	dataset := ""
	if handle != nil && isAgentTimeDimension(handle.Field) {
		dimensionType = AgentGroupByTimeDimension
	}
	if handle != nil {
		dataset = handle.Dataset
	}
	groupings, err := agentCanonicalGroupings(modelName, model, handle)
	if err != nil {
		return AgentDimensionSummary{}, err
	}
	evidence := AssetGovernanceEvidence{}
	if handle != nil && handle.Field != nil {
		evidence, err = governanceEvidence(handle.Field.CustomExtensions)
		if err != nil {
			return AgentDimensionSummary{}, err
		}
	}
	return AgentDimensionSummary{
		Ref: "dimension:" + modelName + "." + qualified, Name: qualified,
		Model: modelName, ModelRef: "model:" + modelName, Dataset: dataset,
		Type: dimensionType, CanonicalGroupings: groupings, Governance: evidence,
	}, nil
}

func agentDimensionSearchValues(summary AgentDimensionSummary) []string {
	values := []string{summary.Ref, summary.Name, summary.Model, summary.ModelRef, summary.Dataset, string(summary.Type)}
	for _, grouping := range summary.CanonicalGroupings {
		values = append(values,
			string(grouping.Grain), grouping.GroupBy.Name, string(grouping.GroupBy.Type), grouping.PhysicalBucket,
		)
		if grouping.GroupBy.Grain != nil {
			values = append(values, string(*grouping.GroupBy.Grain))
		}
	}
	return values
}

func agentCanonicalGroupings(modelName string, model *manifest.ModelIndex, dimension *manifest.FieldHandle) ([]AgentCanonicalGrouping, error) {
	if model == nil || model.Model == nil || dimension == nil || dimension.Field == nil {
		return nil, nil
	}
	calendar, ok, err := ossie.CustomCalendar(model.Model)
	if err != nil {
		return nil, fmt.Errorf("resolve Agent canonical groupings: %w", err)
	}
	if !ok || dimension.Dataset != calendar.Dataset || dimension.Field.Name != calendar.BaseTime {
		return nil, nil
	}
	baseRef := "dimension:" + modelName + "." + dimension.Dataset + "." + dimension.Field.Name
	groupings := make([]AgentCanonicalGrouping, 0, len(calendar.Grains))
	for _, grain := range calendar.Grains {
		grainName := query.TimeGrain(grain.Name)
		groupings = append(groupings, AgentCanonicalGrouping{
			Grain: grainName,
			GroupBy: AgentGroupByParam{
				Name: baseRef, Type: AgentGroupByTimeDimension, Grain: &grainName,
			},
			PhysicalBucket: "dimension:" + modelName + "." + calendar.Dataset + "." + grain.BucketDimension,
		})
	}
	return groupings, nil
}

func validateAgentGroupBy(modelName string, model *manifest.ModelIndex, qualified string, requested AgentGroupByParam) error {
	if model == nil {
		return &serrors.Error{Code: serrors.ErrModelNotFound, Message: "model not found", Details: map[string]any{"model": modelName}}
	}
	handle := model.Fields[qualified]
	if handle == nil || handle.Field == nil || handle.Field.Dimension == nil {
		return agentDimensionNotFoundError(requested.Name)
	}
	actualType := AgentGroupByDimension
	if isAgentTimeDimension(handle.Field) {
		actualType = AgentGroupByTimeDimension
	}
	canonicalRef := "dimension:" + modelName + "." + qualified
	if requested.Grain != nil {
		canonical, ok, err := canonicalGroupingForGrain(modelName, model, *requested.Grain)
		if err != nil {
			return err
		}
		if ok && canonical.GroupBy.Name != canonicalRef {
			return customCalendarGroupingError(requested, canonical)
		}
	}
	if requested.Type != actualType {
		return &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: "group_by type does not match the selected dimension",
			Details: map[string]any{
				"dimension": requested.Name, "provided_type": requested.Type, "expected_type": actualType,
				"remediation": map[string]any{"group_by": AgentGroupByParam{Name: canonicalRef, Type: actualType, Grain: requested.Grain}},
			},
			Suggestions: []string{canonicalRef},
		}
	}
	if requested.Grain == nil {
		return nil
	}
	if actualType != AgentGroupByTimeDimension {
		return &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: "grain is only valid for time_dimension group_by entries",
			Details: map[string]any{
				"dimension":   requested.Name,
				"remediation": map[string]any{"group_by": AgentGroupByParam{Name: canonicalRef, Type: actualType}},
			},
			Suggestions: []string{canonicalRef},
		}
	}
	if !supportsBuiltinTimeGrains(handle.Field) {
		return &serrors.Error{
			Code:    serrors.ErrIncompatibleQueryGrain,
			Message: "time grain requires a temporal dimension data type",
			Details: map[string]any{
				"dimension": requested.Name, "data_type": handle.Field.Datatype,
				"remediation": map[string]any{"group_by": AgentGroupByParam{Name: canonicalRef, Type: actualType}},
			},
			Suggestions: []string{canonicalRef},
		}
	}
	validGrains, _, err := agentTimeGrainMetadata(model, handle)
	if err != nil {
		return err
	}
	for _, valid := range validGrains {
		if valid == *requested.Grain {
			return nil
		}
	}
	canonical, ok, err := canonicalGroupingForGrain(modelName, model, *requested.Grain)
	if err != nil {
		return err
	}
	if ok {
		return customCalendarGroupingError(requested, canonical)
	}
	return &serrors.Error{
		Code:        serrors.ErrIncompatibleQueryGrain,
		Message:     "time grain is not valid for the selected dimension",
		Details:     map[string]any{"dimension": requested.Name, "grain": *requested.Grain, "valid_grains": validGrains},
		Suggestions: []string{canonicalRef},
	}
}

func customCalendarGroupingError(requested AgentGroupByParam, canonical AgentCanonicalGrouping) error {
	return &serrors.Error{
		Code:    serrors.ErrIncompatibleQueryGrain,
		Message: "custom calendar grain must use its canonical base time dimension",
		Details: map[string]any{
			"dimension": requested.Name, "grain": *requested.Grain,
			"remediation": map[string]any{
				"group_by": canonical.GroupBy, "physical_bucket": canonical.PhysicalBucket,
				"do_not_group_by": requested.Name,
			},
		},
		Suggestions: []string{canonical.GroupBy.Name},
	}
}

func canonicalGroupingForGrain(modelName string, model *manifest.ModelIndex, grain query.TimeGrain) (AgentCanonicalGrouping, bool, error) {
	if model == nil || model.Model == nil {
		return AgentCanonicalGrouping{}, false, nil
	}
	calendar, ok, err := ossie.CustomCalendar(model.Model)
	if err != nil || !ok {
		return AgentCanonicalGrouping{}, false, err
	}
	handle := model.Fields[calendar.Dataset+"."+calendar.BaseTime]
	groupings, err := agentCanonicalGroupings(modelName, model, handle)
	if err != nil {
		return AgentCanonicalGrouping{}, false, err
	}
	for _, grouping := range groupings {
		if grouping.Grain == grain {
			return grouping, true, nil
		}
	}
	return AgentCanonicalGrouping{}, false, nil
}

func splitAgentRef(project *manifest.ProjectIndex, body string) (string, string, bool) {
	modelNames := make([]string, 0, len(project.Models))
	for model := range project.Models {
		modelNames = append(modelNames, model)
	}
	sort.Slice(modelNames, func(i, j int) bool {
		if len(modelNames[i]) != len(modelNames[j]) {
			return len(modelNames[i]) > len(modelNames[j])
		}
		return modelNames[i] < modelNames[j]
	})
	for _, model := range modelNames {
		prefix := model + "."
		if strings.HasPrefix(body, prefix) && len(body) > len(prefix) {
			return model, strings.TrimPrefix(body, prefix), true
		}
	}
	return "", "", false
}

func normalizeAgentSemanticRef(project *manifest.ProjectIndex, model, raw, kind string) (string, error) {
	raw = strings.TrimSpace(raw)
	prefix := kind + ":"
	if !strings.HasPrefix(raw, prefix) {
		return raw, nil
	}
	refModel, name, ok := splitAgentRef(project, strings.TrimPrefix(raw, prefix))
	if !ok || refModel != model {
		return "", &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: kind + " reference does not belong to the selected metric model",
			Details: map[string]any{kind: raw, "model": model},
		}
	}
	return name, nil
}

func normalizeAgentFilterField(project *manifest.ProjectIndex, model, raw string) (string, error) {
	if strings.HasPrefix(raw, "dimension:") {
		return normalizeAgentSemanticRef(project, model, raw, "dimension")
	}
	if strings.HasPrefix(raw, "metric:") {
		return normalizeAgentSemanticRef(project, model, raw, "metric")
	}
	return raw, nil
}

func normalizeAgentOrderField(project *manifest.ProjectIndex, model, raw string) (string, error) {
	return normalizeAgentFilterField(project, model, raw)
}
