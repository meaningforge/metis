package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	attributionanalytics "github.com/meaningforge/metis/analytics/attribution"
	comparisonanalytics "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/observability"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NewHTTPHandler builds the official MCP Streamable HTTP handler. The handler
// is transport-only: semantic behavior remains in the shared service layer.
func NewHTTPHandler(discoveryService *service.DiscoveryService, compileService *service.CompileService) http.Handler {
	return NewObservedHTTPHandler(discoveryService, compileService, nil, nil)
}

// NewHTTPHandlerWithQueryMetrics adds the governed semantic execution tool to
// the normal MCP surface without changing legacy embedder construction.
func NewHTTPHandlerWithQueryMetrics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService) http.Handler {
	return NewObservedHTTPHandlerWithQueryMetrics(discoveryService, compileService, queryMetrics, nil, nil)
}

// NewServer returns the production Metis MCP server before transport wrapping.
// It exists so isolated harnesses can compose benchmark-only tools without
// changing the tool surface registered by the Metis application process.
func NewServer(discoveryService *service.DiscoveryService, compileService *service.CompileService) *mcp.Server {
	return newServer(discoveryService, compileService, nil, nil)
}

func NewServerWithQueryMetrics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService) *mcp.Server {
	return newServerWithQueryMetrics(discoveryService, compileService, queryMetrics, nil, nil)
}

// NewObservedHTTPHandler adds bounded MCP tool observations while preserving
// the same shared service behavior as the default no-op handler.
func NewObservedHTTPHandler(discoveryService *service.DiscoveryService, compileService *service.CompileService, recorder *observability.Recorder, tracing *observability.Tracing) http.Handler {
	return NewObservedHTTPHandlerWithQueryMetrics(discoveryService, compileService, nil, recorder, tracing)
}

// NewObservedHTTPHandlerWithQueryMetrics adds bounded observation to both
// compile_sql and query_metrics while preserving the shared service layer.
func NewObservedHTTPHandlerWithQueryMetrics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, recorder *observability.Recorder, tracing *observability.Tracing) http.Handler {
	return NewObservedHTTPHandlerWithExecution(discoveryService, compileService, queryMetrics, nil, recorder, tracing)
}

// NewObservedHTTPHandlerWithExecution composes every configured governed
// execution capability. A nil attribution service keeps attribute_metric out
// of tools/list for runtime-disabled embedders.
func NewObservedHTTPHandlerWithExecution(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, attributeMetric *service.AttributeMetricService, recorder *observability.Recorder, tracing *observability.Tracing) http.Handler {
	return NewObservedHTTPHandlerWithAnalytics(discoveryService, compileService, queryMetrics, attributeMetric, nil, recorder, tracing)
}

// NewObservedHTTPHandlerWithAnalytics composes all production analytical
// workflows while keeping legacy embedder constructors source-compatible.
func NewObservedHTTPHandlerWithAnalytics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, attributeMetric *service.AttributeMetricService, compareMetrics *service.CompareMetricsService, recorder *observability.Recorder, tracing *observability.Tracing) http.Handler {
	return NewObservedHTTPHandlerWithDimensionValues(discoveryService, compileService, queryMetrics, nil, attributeMetric, compareMetrics, recorder, tracing)

}

// NewObservedHTTPHandlerWithDimensionValues composes the complete production
// Agent surface, including bounded live dimension-member discovery. The older
// constructor remains source-compatible for embedders that do not expose it.
func NewObservedHTTPHandlerWithDimensionValues(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, dimensionValues *service.DimensionValuesService, attributeMetric *service.AttributeMetricService, compareMetrics *service.CompareMetricsService, recorder *observability.Recorder, tracing *observability.Tracing, managers ...*runtimeservice.Manager) http.Handler {
	server := NewObservedServerWithDimensionValues(discoveryService, compileService, queryMetrics, dimensionValues, attributeMetric, compareMetrics, recorder, tracing, managers...)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// NewObservedServerWithDimensionValues returns the complete production MCP
// server before a transport is selected. HTTP and stdio entrypoints share this
// exact registration path so tools, schemas, errors, and observations cannot
// drift by transport.
func NewObservedServerWithDimensionValues(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, dimensionValues *service.DimensionValuesService, attributeMetric *service.AttributeMetricService, compareMetrics *service.CompareMetricsService, recorder *observability.Recorder, tracing *observability.Tracing, managers ...*runtimeservice.Manager) *mcp.Server {
	var manager *runtimeservice.Manager
	if len(managers) > 0 {
		manager = managers[0]
	}
	return newServerWithDimensionValues(discoveryService, compileService, queryMetrics, dimensionValues, attributeMetric, compareMetrics, recorder, tracing, manager)
}

func newServer(discoveryService *service.DiscoveryService, compileService *service.CompileService, recorder *observability.Recorder, tracing *observability.Tracing) *mcp.Server {
	return newServerWithQueryMetrics(discoveryService, compileService, nil, recorder, tracing)
}

func newServerWithQueryMetrics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, recorder *observability.Recorder, tracing *observability.Tracing) *mcp.Server {
	return newServerWithExecution(discoveryService, compileService, queryMetrics, nil, recorder, tracing)
}

func newServerWithExecution(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, attributeMetric *service.AttributeMetricService, recorder *observability.Recorder, tracing *observability.Tracing) *mcp.Server {
	return newServerWithAnalytics(discoveryService, compileService, queryMetrics, attributeMetric, nil, recorder, tracing)
}

func newServerWithAnalytics(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, attributeMetric *service.AttributeMetricService, compareMetrics *service.CompareMetricsService, recorder *observability.Recorder, tracing *observability.Tracing) *mcp.Server {
	return newServerWithDimensionValues(discoveryService, compileService, queryMetrics, nil, attributeMetric, compareMetrics, recorder, tracing, nil)
}

func newServerWithDimensionValues(discoveryService *service.DiscoveryService, compileService *service.CompileService, queryMetrics *service.QueryMetricsService, dimensionValues *service.DimensionValuesService, attributeMetric *service.AttributeMetricService, compareMetrics *service.CompareMetricsService, recorder *observability.Recorder, tracing *observability.Tracing, managers ...*runtimeservice.Manager) *mcp.Server {
	var manager *runtimeservice.Manager
	if len(managers) > 0 {
		manager = managers[0]
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "metis", Version: version.Version}, &mcp.ServerOptions{Instructions: agentSemanticInstructions})
	router := generationRouter{fallback: runtimeservice.SemanticServices{
		Discovery: discoveryService, Compile: compileService, QueryMetrics: queryMetrics,
		DimensionValues: dimensionValues, AttributeMetric: attributeMetric, CompareMetrics: compareMetrics,
	}, manager: manager}

	mcp.AddTool(server, &mcp.Tool{Name: ToolSearchOntologyConcepts, Description: "Find ontology concepts using bounded metadata terms. Use when a concept identity is unknown; pass a returned concept_ref to resolve_ontology_concept.", Annotations: readOnlyAnnotations("Search ontology concepts")}, observeTool(observability.MCPMethodSearchOntologyConcepts, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.OntologyConceptSearchRequest) (*mcp.CallToolResult, *service.OntologyConceptSearchResult, error) {
		project, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = project
		result, err := services.Discovery.SearchOntologyConcepts(ctx, input)
		return nil, result, err
	}))
	mcp.AddTool(server, &mcp.Tool{Name: ToolResolveOntologyConcept, Description: "Resolve one exact ontology concept_ref to visible canonical dimension candidates with evidence. Even a unique result requires explicit caller selection; never infer a metric or rewrite a query.", Annotations: readOnlyAnnotations("Resolve ontology concept")}, observeTool(observability.MCPMethodResolveOntologyConcept, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.OntologyResolutionRequest) (*mcp.CallToolResult, *service.OntologyResolutionResult, error) {
		project, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = project
		result, err := services.Discovery.ResolveOntologyConcept(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolListProjects,
		Description: "List visible projects and their available capabilities. Use only when project context or execution availability is unknown.",
		Annotations: readOnlyAnnotations("List projects"),
	}, observeTool(observability.MCPMethodListProjects, recorder, tracing, func(ctx context.Context, _ *mcp.CallToolRequest, input service.ListProjectsRequest) (*mcp.CallToolResult, *service.ListProjectsResult, error) {
		result, err := router.listProjects(ctx, input)
		return nil, result, err
	}))

	if dimensionValues != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:        ToolGetDimensionValues,
			Description: "Execute bounded live discovery of values for one selected canonical dimension, optionally scoped by compatible metrics. Use only to resolve filter-member wording that may not match stored values. Do not use it for grouping, ordering, top-k, or limit discovery; submit those directly through semantic query fields. Results are current warehouse evidence, not static metadata.",
			InputSchema: dimensionValuesInputSchema(),
			Annotations: &mcp.ToolAnnotations{Title: "Get dimension values", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
		}, observeTool(observability.MCPMethodGetDimensionValues, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.DimensionValuesQuery) (*mcp.CallToolResult, *service.DimensionValuesResult, error) {
			explicitProject := input.ProjectID != ""
			projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
			recordProjectContextSource(ctx, source)
			if err != nil {
				return nil, nil, err
			}
			input.ProjectID = projectID
			input.MarkProjectContextInherited(!explicitProject)
			result, err := services.DimensionValues.GetDimensionValues(ctx, input)
			return nil, result, err
		}))
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolListModels,
		Description: "Find semantic-model candidates in the active project. Use when model scope is needed; call get_model only for a selected candidate.",
		Annotations: readOnlyAnnotations("List semantic models"),
	}, observeTool(observability.MCPMethodListModels, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.ListModelsRequest) (*mcp.CallToolResult, *service.ListModelsResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).ListModels(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetModel,
		Description: "Get authored context and compact asset inventories for one selected canonical model ref.",
		Annotations: readOnlyAnnotations("Get semantic model"),
	}, observeTool(observability.MCPMethodGetModel, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentGetModelRequest) (*mcp.CallToolResult, *service.AgentGetModelResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).GetModel(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolListMetrics,
		Description: "Find governed metric candidates. Ordering is retrieval-only and never selects a metric; when selection_required is true, compare the returned evidence and select an exact canonical ref or ask the user to clarify. Typed constraints are selection evidence that Metis applies automatically.",
		InputSchema: listMetricsInputSchema,
		Annotations: readOnlyAnnotations("List metrics"),
	}, observeTool(observability.MCPMethodListMetrics, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input listMetricsInput) (*mcp.CallToolResult, *service.ListMetricsResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).ListMetrics(ctx, input.serviceRequest(projectID))
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetMetric,
		Description: "Get authored context and typed semantic constraints for one selected canonical metric. Metis applies those constraints automatically; use get_dimensions for compatible groupings.",
		Annotations: readOnlyAnnotations("Get metric"),
	}, observeTool(observability.MCPMethodGetMetric, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentGetMetricRequest) (*mcp.CallToolResult, *service.AgentGetMetricResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).GetMetric(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetDimensions,
		Description: "List a bounded page of dimensions compatible with every selected metric, or dimensions for one model in metric-free queries. Use narrow search terms, limit, and next_cursor for progressive discovery. Returns canonical group_by shapes accepted by compile_sql and query_metrics; use get_dimension only for selected detail.",
		Annotations: readOnlyAnnotations("Get compatible dimensions"),
	}, observeTool(observability.MCPMethodGetDimensions, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.GetDimensionsRequest) (*mcp.CallToolResult, *service.GetDimensionsResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).GetDimensions(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetDimension,
		Description: "Get authored details and valid grains for one selected canonical dimension. Use canonical_groupings for custom-calendar grouping.",
		Annotations: readOnlyAnnotations("Get dimension"),
	}, observeTool(observability.MCPMethodGetDimension, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentGetDimensionRequest) (*mcp.CallToolResult, *service.AgentGetDimensionResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).GetDimension(ctx, input)
		return nil, result, err
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetRelationships,
		Description: "Get authored relationship evidence for one selected canonical model, optionally narrowed by dataset or search. Use for cross-dataset and point-in-time questions.",
		Annotations: readOnlyAnnotations("Get relationships"),
	}, observeTool(observability.MCPMethodGetRelationships, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentGetRelationshipsRequest) (*mcp.CallToolResult, *service.AgentGetRelationshipsResult, error) {
		projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
		recordProjectContextSource(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		input.ProjectID = projectID
		result, err := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics).GetRelationships(ctx, input)
		return nil, result, err
	}))

	if compileService != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:        ToolCompile,
			Description: "Compile selected canonical semantic refs into a validated physical query without executing it. dialect controls SQL syntax only and never selects a connection. structuredContent returns the complete compiled query and output schema.",
			InputSchema: compileInputSchema(),
			Annotations: readOnlyAnnotations("Compile semantic query"),
		}, observeTool(observability.MCPMethodCompile, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentCompileRequest) (*mcp.CallToolResult, *artifact.CompiledQuery, error) {
			explicitProject := input.ProjectID != ""
			projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
			recordProjectContextSource(ctx, source)
			if err != nil {
				return nil, nil, err
			}
			input.ProjectID = projectID
			input.MarkProjectContextInherited(!explicitProject)
			agentSemantics := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics)
			compileRequest, err := agentSemantics.BuildCompileRequestContext(ctx, input)
			if err != nil {
				return nil, nil, err
			}
			result, err := services.Compile.Compile(ctx, compileRequest)
			if err != nil {
				return nil, nil, err
			}
			toolResult, err := compileToolResult(result)
			return toolResult, result, err
		}))
	}

	if queryMetrics != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:        ToolQueryMetrics,
			Description: "Execute a bounded governed metric query against the active project's configured DataSource. Use for result rows; use compile_sql only when external physical SQL is requested. Accepts semantic intent only and applies governed metric behavior automatically.",
			InputSchema: queryMetricsInputSchema(),
			Annotations: &mcp.ToolAnnotations{Title: "Query semantic metrics", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
		}, observeTool(observability.MCPMethodQueryMetrics, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.AgentQueryMetricsRequest) (*mcp.CallToolResult, *service.QueryMetricsResult, error) {
			explicitProject := input.ProjectID != ""
			projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
			recordProjectContextSource(ctx, source)
			if err != nil {
				return nil, nil, err
			}
			input.ProjectID = projectID
			input.MarkProjectContextInherited(!explicitProject)
			agentSemantics := service.NewAgentSemanticService(services.Discovery, services.QueryMetrics)
			queryRequest, err := agentSemantics.BuildQueryMetricsRequestContext(ctx, input)
			if err != nil {
				return nil, nil, err
			}
			result, err := services.QueryMetrics.QueryMetrics(ctx, queryRequest)
			return nil, result, err
		}))
	}

	if attributeMetric != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:         ToolAttributeMetric,
			Description:  "Attribute one governed metric's change between exact baseline and current periods across selected dimensions. Results are contribution evidence, not business causality; do not compare contribution ranks across dimensions.",
			InputSchema:  attributeMetricInputSchema(),
			OutputSchema: attributeMetricOutputSchema(),
			Annotations:  &mcp.ToolAnnotations{Title: "Attribute metric change", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
		}, observeTool(observability.MCPMethodAttributeMetric, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.MetricAttributionQuery) (*mcp.CallToolResult, *attributionanalytics.AttributeMetricResult, error) {
			projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
			recordProjectContextSource(ctx, source)
			if err != nil {
				return nil, nil, err
			}
			input.ProjectID = projectID
			result, err := services.AttributeMetric.AttributeMetric(ctx, input)
			return nil, result, err
		}))
	}

	if compareMetrics != nil {
		mcp.AddTool(server, &mcp.Tool{
			Name:         ToolCompareMetrics,
			Description:  "Compare governed metrics between exact baseline and current periods at one shared dimension grain. Returns values, delta, and percent change; absent rows, null values, and zero remain distinct.",
			InputSchema:  compareMetricsInputSchema(),
			OutputSchema: compareMetricsOutputSchema(),
			Annotations:  &mcp.ToolAnnotations{Title: "Compare semantic metrics", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
		}, observeTool(observability.MCPMethodCompareMetrics, recorder, tracing, func(ctx context.Context, request *mcp.CallToolRequest, input service.MetricComparisonQuery) (*mcp.CallToolResult, *comparisonanalytics.Result, error) {
			projectID, source, services, err := router.resolve(ctx, input.ProjectID, request)
			recordProjectContextSource(ctx, source)
			if err != nil {
				return nil, nil, err
			}
			input.ProjectID = projectID
			result, err := services.CompareMetrics.CompareMetrics(ctx, input)
			return nil, result, err
		}))
	}

	return server
}

type generationRouter struct {
	fallback runtimeservice.SemanticServices
	manager  *runtimeservice.Manager
}

func (r generationRouter) resolve(ctx context.Context, explicit string, request *mcp.CallToolRequest) (string, projectContextSource, runtimeservice.SemanticServices, error) {
	if r.manager == nil {
		projectID, source, err := resolveToolProjectIDWithDeployment(explicit, request, r.fallback.Discovery)
		return projectID, source, r.fallback, err
	}
	projectID, source, err := resolveToolProjectID(explicit, request)
	if err != nil {
		var semanticErr *serrors.Error
		if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectRequired {
			return "", source, runtimeservice.SemanticServices{}, err
		}
		projectID, err = r.manager.ResolveProject("")
		source = projectSourceDefault
	} else {
		projectID, err = r.manager.ResolveProject(projectID)
	}
	if err != nil {
		return "", source, runtimeservice.SemanticServices{}, err
	}
	generation, pinned, err := runtimeservice.FromContext(ctx, projectID)
	if err != nil {
		return "", source, runtimeservice.SemanticServices{}, err
	}
	if !pinned {
		generation = r.manager.Current(projectID)
	}
	if generation == nil {
		return "", source, runtimeservice.SemanticServices{}, serrors.Internal("project semantic generation is unavailable", nil)
	}
	return projectID, source, generation.SemanticServices, nil
}

func (r generationRouter) listProjects(ctx context.Context, input service.ListProjectsRequest) (*service.ListProjectsResult, error) {
	if r.manager == nil {
		return service.NewAgentSemanticService(r.fallback.Discovery, r.fallback.QueryMetrics).ListProjects(ctx, input)
	}
	projects := make([]service.AgentProjectSummary, 0, len(r.manager.ProjectIDs()))
	for _, projectID := range r.manager.ProjectIDs() {
		generation, pinned, err := runtimeservice.FromContext(ctx, projectID)
		if err != nil {
			return nil, err
		}
		if !pinned {
			generation = r.manager.Current(projectID)
		}
		if generation == nil {
			return nil, serrors.Internal("project semantic generation is unavailable", nil)
		}
		result, err := service.NewAgentSemanticService(generation.Discovery, generation.QueryMetrics).ListProjects(ctx, input)
		if err != nil {
			return nil, err
		}
		projects = append(projects, result.Projects...)
	}
	return &service.ListProjectsResult{Projects: projects}, nil
}

func compileToolResult(compiled *artifact.CompiledQuery) (*mcp.CallToolResult, error) {
	if compiled == nil {
		return nil, fmt.Errorf("compiled query is required")
	}
	physicalQuery, err := json.Marshal(compiled.SQLRenderResult)
	if err != nil {
		return nil, fmt.Errorf("marshal physical query for MCP content: %w", err)
	}
	outputSchema, err := json.Marshal(compiled.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal output schema for MCP content: %w", err)
	}
	var content strings.Builder
	content.WriteString("Physical query:\n")
	content.Write(physicalQuery)
	content.WriteString("\nOutput schema:\n")
	content.Write(outputSchema)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: content.String()}}}, nil
}

const agentSemanticInstructions = `Metis resolves governed semantic intent into compiled SQL or bounded query results. Omit project_id when active project context is configured; a model name is never a project ID. Discover only missing identities: list_models optionally narrows model scope, while list_metrics retrieves candidates but never selects one by ranking. definition_equivalent_refs reports identical authored definitions but never selects the business identity. When selection_required is true, choose an exact canonical ref from returned evidence or ask the user to clarify. After metric selection, call get_dimensions with narrow search terms and a bounded limit only for needed grouping or filter compatibility; follow next_cursor only when needed. Selected detail tools are optional. Use get_dimension_values only when filter wording may not match current stored members, never for grouping, ordering, or limit discovery. Use query_metrics when result rows are needed and it is available; use compile_sql only when physical SQL is explicitly requested for external execution. Submit every requested output, grouping, filter, ordering, and limit as semantic intent. Metis applies governed cumulative, offset, conversion, fill, and semi-additive behavior; never reproduce it in handwritten SQL. Preserve canonical refs and returned group_by types and grains. For custom calendars use canonical_groupings[].group_by, never physical_bucket.`

func readOnlyAnnotations(title string) *mcp.ToolAnnotations {
	value := false
	return &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: true, IdempotentHint: true,
		DestructiveHint: &value, OpenWorldHint: &value,
	}
}

func boolPointer(value bool) *bool { return &value }

func recordProjectContextSource(ctx context.Context, source projectContextSource) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("metis.project.source", string(source)))
}

func observeTool[Input, Output any](method observability.MCPMethod, recorder *observability.Recorder, tracing *observability.Tracing, handler mcp.ToolHandlerFor[Input, Output]) mcp.ToolHandlerFor[Input, Output] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input Input) (toolResult *mcp.CallToolResult, result Output, err error) {
		started := time.Now()
		ctx, span := tracing.Start(ctx, observability.OperationMCP, trace.WithAttributes(attribute.String("rpc.method", string(method))))
		completed := false
		defer func() {
			observationErr := err
			if observationErr == nil && !completed {
				observationErr = errors.New("MCP tool terminated before completion")
			}
			observation := observability.MCPObservation{Method: method, Result: observability.ResultSuccess, Duration: time.Since(started)}
			if observationErr != nil {
				observation.Result = observability.ResultError
				tracing.RecordError(ctx, observationErr, observability.ErrorCode(observationErr))
			}
			recorder.Record(ctx, observation)
			span.End()
			if completed {
				err = encodeToolError(err)
			}
		}()
		toolResult, result, err = handler(ctx, request, input)
		completed = true
		return toolResult, result, err
	}
}
