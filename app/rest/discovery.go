package rest

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/httperr"
	"github.com/meaningforge/metis/app/observability"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
)

// RegisterDiscoveryRoutes mounts transport-only discovery handlers on a Gin router group.
func RegisterDiscoveryRoutes(group *gin.RouterGroup, discoveryService *service.DiscoveryService) {
	h := &DiscoveryHandler{service: discoveryService}
	group.GET("/projects/:project/semantics/search", h.search)
	group.GET("/projects/:project/models/:model", h.getModel)
	group.GET("/projects/:project/models/:model/metrics/:metric", h.getMetric)
	group.GET("/projects/:project/models/:model/metrics/:metric/dimensions", h.getMetricDimensions)
	group.GET("/projects/:project/models/:model/dimensions/:dimension", h.getDimension)
	group.GET("/projects/:project/models/:model/relationships", h.getRelationships)
	RegisterAgentSemanticRoutes(group, discoveryService)
	RegisterSemanticContextRoutes(group, discoveryService)
}

type DiscoveryHandler struct {
	service *service.DiscoveryService
}

func (h *DiscoveryHandler) search(c *gin.Context) {
	limit, err := searchLimit(c.Query("limit"))
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.SearchSemantics(c.Request.Context(), service.SearchSemanticsRequest{
		Project: c.Param("project"),
		Query:   c.Query("q"),
		Model:   c.Query("model"),
		Kinds:   searchKinds(c.QueryArray("kind")),
		Limit:   limit,
	})
	writeJSON(c, result, err)
}

func searchKinds(values []string) []service.AssetKind {
	var kinds []service.AssetKind
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				kinds = append(kinds, service.AssetKind(part))
			}
		}
	}
	return kinds
}

func searchLimit(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "semantic search limit must be an integer", Details: map[string]any{"limit": value}}
	}
	return limit, nil
}

func (h *DiscoveryHandler) getModel(c *gin.Context) {
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.GetModel(c.Request.Context(), service.GetModelRequest{Project: c.Param("project"), Model: c.Param("model")})
	writeJSON(c, result, err)
}

func (h *DiscoveryHandler) getMetric(c *gin.Context) {
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.GetMetric(c.Request.Context(), service.GetMetricRequest{Project: c.Param("project"), Model: c.Param("model"), Metric: c.Param("metric")})
	writeJSON(c, result, err)
}

func (h *DiscoveryHandler) getMetricDimensions(c *gin.Context) {
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.GetMetricDimensions(c.Request.Context(), service.GetMetricDimensionsRequest{
		Project: c.Param("project"), Model: c.Param("model"), Metric: c.Param("metric"),
	})
	writeJSON(c, result, err)
}

func (h *DiscoveryHandler) getDimension(c *gin.Context) {
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.GetDimension(c.Request.Context(), service.GetDimensionRequest{Project: c.Param("project"), Model: c.Param("model"), Dimension: c.Param("dimension")})
	writeJSON(c, result, err)
}

func (h *DiscoveryHandler) getRelationships(c *gin.Context) {
	selected, err := discoveryFor(c.Request.Context(), c.Param("project"), h.service)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := selected.GetRelationships(c.Request.Context(), service.GetRelationshipsRequest{Project: c.Param("project"), Model: c.Param("model")})
	writeJSON(c, result, err)
}

func writeJSON(c *gin.Context, value any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, value)
		return
	}

	body := serrors.PayloadFrom(err)
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	observability.RecordError(ctx, err, serrors.ErrorCode(body.Code))
	c.JSON(httperr.StatusOf(serrors.ErrorCode(body.Code)), body)
}
