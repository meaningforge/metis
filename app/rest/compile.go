package rest

import (
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
)

func RegisterCompileRoutes(group *gin.RouterGroup, compileService *service.CompileService) {
	h := &CompileHandler{service: compileService}
	group.POST("/compile-sql", h.compile)
	group.POST("/explain", h.explain)
	group.POST("/validate", h.validate)
}

// RegisterQueryMetricsRoutes mounts the governed semantic execution endpoint.
// It is registered even for compile-only deployments so clients receive the
// stable unavailable error rather than inferring capability from routing.
func RegisterQueryMetricsRoutes(group *gin.RouterGroup, queryMetrics *service.QueryMetricsService) {
	h := &QueryMetricsHandler{service: queryMetrics}
	group.POST("/query-metrics", h.queryMetrics)
}

// RegisterDimensionValuesRoutes mounts bounded live dimension-member
// discovery. Compile-only deployments keep the route and return the stable
// execution-unavailable contract.
func RegisterDimensionValuesRoutes(group *gin.RouterGroup, dimensionValues *service.DimensionValuesService) {
	h := &DimensionValuesHandler{service: dimensionValues}
	group.POST("/dimension-values", h.dimensionValues)
}

// RegisterAttributeMetricRoutes mounts the complete deterministic attribution
// runtime operation. Compile-only deployments retain the route and return the
// stable execution-unavailable error.
func RegisterAttributeMetricRoutes(group *gin.RouterGroup, attributeMetric *service.AttributeMetricService) {
	h := &AttributeMetricHandler{service: attributeMetric}
	group.POST("/attribute-metric", h.attributeMetric)
}

// RegisterCompareMetricsRoutes mounts deterministic governed period
// comparison. Compile-only deployments keep the stable unavailable contract.
func RegisterCompareMetricsRoutes(group *gin.RouterGroup, compareMetrics *service.CompareMetricsService) {
	h := &CompareMetricsHandler{service: compareMetrics}
	group.POST("/compare-metrics", h.compareMetrics)
}

type CompileHandler struct {
	service *service.CompileService
}

type QueryMetricsHandler struct {
	service *service.QueryMetricsService
}

type DimensionValuesHandler struct {
	service *service.DimensionValuesService
}

type AttributeMetricHandler struct {
	service *service.AttributeMetricService
}

type CompareMetricsHandler struct {
	service *service.CompareMetricsService
}

func (h *CompileHandler) compile(c *gin.Context) {
	var req service.CompileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid compile request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := compileFor(c.Request.Context(), req.Query.Project, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.Compile(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *CompileHandler) explain(c *gin.Context) {
	var req service.CompileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid explain request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := compileFor(c.Request.Context(), req.Query.Project, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.Explain(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *CompileHandler) validate(c *gin.Context) {
	var req service.CompileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid validate request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := compileFor(c.Request.Context(), req.Query.Project, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.Validate(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *QueryMetricsHandler) queryMetrics(c *gin.Context) {
	var req service.QueryMetricsRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid query_metrics request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid query_metrics request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := queryMetricsFor(c.Request.Context(), req.Query.Project, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	if selected == nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrQueryExecutionUnavailable, Message: "query execution is not configured"})
		return
	}
	result, err := selected.QueryMetrics(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *DimensionValuesHandler) dimensionValues(c *gin.Context) {
	var req service.DimensionValuesQuery
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid get_dimension_values request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid get_dimension_values request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := dimensionValuesFor(c.Request.Context(), req.ProjectID, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	if selected == nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrQueryExecutionUnavailable, Message: "dimension value execution is not configured"})
		return
	}
	result, err := selected.GetDimensionValues(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *AttributeMetricHandler) attributeMetric(c *gin.Context) {
	var req service.MetricAttributionQuery
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid attribute_metric request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid attribute_metric request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := attributeMetricFor(c.Request.Context(), req.ProjectID, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	if selected == nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrQueryExecutionUnavailable, Message: "metric attribution execution is not configured"})
		return
	}
	result, err := selected.AttributeMetric(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *CompareMetricsHandler) compareMetrics(c *gin.Context) {
	var req service.MetricComparisonQuery
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid compare_metrics request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid compare_metrics request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	selected, err := compareMetricsFor(c.Request.Context(), req.ProjectID, h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	if selected == nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrQueryExecutionUnavailable, Message: "metric comparison execution is not configured"})
		return
	}
	result, err := selected.CompareMetrics(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func requireSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
