package rest

import (
	"github.com/gin-gonic/gin"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
)

// RegisterAgentSemanticRoutes exposes the compact Agent search service through
// REST without duplicating its SemanticManifest behavior.
func RegisterAgentSemanticRoutes(group *gin.RouterGroup, discovery *service.DiscoveryService) {
	h := &AgentSemanticHandler{discovery: discovery}
	group.POST("/projects/:project/semantic/search", h.searchSemantics)
	group.POST("/projects/:project/ontology/search", h.searchOntology)
	group.POST("/projects/:project/ontology/resolve", h.resolveOntology)
}

type AgentSemanticHandler struct {
	discovery *service.DiscoveryService
}

func (h *AgentSemanticHandler) searchOntology(c *gin.Context) {
	var req service.OntologyConceptSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid ontology search request"})
		return
	}
	req.ProjectID = c.Param("project")
	discovery, err := discoveryFor(c.Request.Context(), req.ProjectID, h.discovery)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := discovery.SearchOntologyConcepts(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *AgentSemanticHandler) resolveOntology(c *gin.Context) {
	var req service.OntologyResolutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid ontology resolution request"})
		return
	}
	req.ProjectID = c.Param("project")
	discovery, err := discoveryFor(c.Request.Context(), req.ProjectID, h.discovery)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	result, err := discovery.ResolveOntologyConcept(c.Request.Context(), req)
	writeJSON(c, result, err)
}

func (h *AgentSemanticHandler) searchSemantics(c *gin.Context) {
	var req service.SemanticSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid semantic search request", Details: map[string]any{"cause": err.Error()}})
		return
	}
	req.Project = c.Param("project")
	discovery, err := discoveryFor(c.Request.Context(), req.Project, h.discovery)
	if err != nil {
		writeJSON(c, nil, err)
		return
	}
	search := service.NewSemanticSearchService(discovery)
	result, err := search.Search(c.Request.Context(), req)
	writeJSON(c, result, err)
}
