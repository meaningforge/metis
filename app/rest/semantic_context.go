package rest

import (
	"github.com/gin-gonic/gin"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
)

// RegisterSemanticContextRoutes exposes the semantic context bundle without
// adding transport-specific semantic behavior.
func RegisterSemanticContextRoutes(group *gin.RouterGroup, discovery *service.DiscoveryService) {
	if discovery == nil {
		return
	}
	h := &SemanticContextHandler{discovery: discovery}
	group.POST("/projects/:project/models/:model/semantic-context", h.get)
}

type SemanticContextHandler struct {
	discovery *service.DiscoveryService
}

func (h *SemanticContextHandler) get(c *gin.Context) {
	var req service.SemanticContextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid semantic context request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	req.Project = c.Param("project")
	req.Model = c.Param("model")
	discovery, err := discoveryFor(c.Request.Context(), req.Project, h.discovery)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	semanticContext := service.NewSemanticContextServiceFromDiscovery(discovery)
	result, err := semanticContext.Get(c.Request.Context(), req)
	writeJSON(c, result, err)
}
