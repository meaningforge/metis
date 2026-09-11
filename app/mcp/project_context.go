package mcp

import (
	"errors"
	"strings"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ProjectMetaKey is deliberately namespaced so it cannot collide with MCP
	// protocol metadata or another server's application context.
	ProjectMetaKey   = "dev.metis/project-id"
	ProjectHeaderKey = "X-Metis-Project-Id"
)

type projectContextSource string

const (
	projectSourceArgument projectContextSource = "argument"
	projectSourceMeta     projectContextSource = "meta"
	projectSourceHeader   projectContextSource = "header"
	projectSourceDefault  projectContextSource = "default"
	projectSourceMissing  projectContextSource = "missing"
)

func resolveToolProjectID(explicit string, request *mcp.CallToolRequest) (string, projectContextSource, error) {
	if projectID := strings.TrimSpace(explicit); projectID != "" {
		return projectID, projectSourceArgument, nil
	}
	if request != nil && request.Params != nil {
		if raw, ok := request.Params.GetMeta()[ProjectMetaKey]; ok {
			projectID, ok := raw.(string)
			if !ok || strings.TrimSpace(projectID) == "" {
				return "", projectSourceMeta, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "MCP project metadata must be a non-empty string", Details: map[string]any{"key": ProjectMetaKey}}
			}
			return strings.TrimSpace(projectID), projectSourceMeta, nil
		}
	}
	if request != nil && request.Extra != nil {
		if projectID := strings.TrimSpace(request.Extra.Header.Get(ProjectHeaderKey)); projectID != "" {
			return projectID, projectSourceHeader, nil
		}
	}
	return "", projectSourceMissing, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project_id is required when no active project context is bound"}
}

func resolveToolProjectIDWithDeployment(explicit string, request *mcp.CallToolRequest, discovery *service.DiscoveryService) (string, projectContextSource, error) {
	projectID, source, err := resolveToolProjectID(explicit, request)
	if err == nil || discovery == nil {
		return projectID, source, err
	}
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectRequired {
		return "", source, err
	}
	projectID, err = discovery.ResolveProject("")
	if err != nil {
		return "", projectSourceDefault, err
	}
	return projectID, projectSourceDefault, nil
}
