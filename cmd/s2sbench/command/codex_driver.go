package command

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

type codexDriver struct {
	binary      string
	model       string
	mcpBaseline mcpEnvironmentBaseline
}

func newCodexDriver(binary, model string) (*codexDriver, error) {
	binary = strings.TrimSpace(binary)
	model = strings.TrimSpace(model)
	if binary == "" {
		binary = "codex"
	}
	if model == "" {
		return nil, fmt.Errorf("codex model is required")
	}
	return &codexDriver{binary: binary, model: model}, nil
}

func (d *codexDriver) Identity(ctx context.Context) (s2sbench.AgentIdentity, error) {
	version, err := commandVersion(ctx, d.binary, "--version")
	if err != nil {
		return s2sbench.AgentIdentity{}, fmt.Errorf("codex version: %w", err)
	}
	identity := s2sbench.AgentIdentity{Name: "codex", Version: version, Model: d.model}
	if err := identity.Validate(); err != nil {
		return s2sbench.AgentIdentity{}, err
	}
	return identity, nil
}

func codexAppServerArgs(mcp *s2sbench.AgentMCP) ([]string, error) {
	args := []string{
		"app-server", "--stdio", "--strict-config",
		// Codex apps are exposed through the built-in codex_apps MCP server even
		// with an empty CODEX_HOME. Disable the feature so raw sessions expose no
		// MCP server and Metis sessions expose only the benchmark-owned server.
		"--disable", "apps",
		"-c", "analytics.enabled=false",
	}
	if mcp == nil {
		return args, nil
	}
	mcpArgs, err := codexMCPArgs(*mcp)
	if err != nil {
		return nil, err
	}
	return append(args, mcpArgs...), nil
}

func codexMCPArgs(mcp s2sbench.AgentMCP) ([]string, error) {
	name := strings.TrimSpace(mcp.Name)
	if !agentNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid MCP name %q", mcp.Name)
	}
	if strings.TrimSpace(mcp.URL) == "" {
		return nil, fmt.Errorf("MCP URL is required")
	}
	prefix := "mcp_servers." + name + "."
	args := []string{
		"-c", prefix + "url=" + strconv.Quote(strings.TrimSpace(mcp.URL)),
		"-c", prefix + "required=true",
		// The app-server is headless and uses approval_policy=Never. Explicitly
		// preapprove the benchmark-owned Metis MCP namespace.
		"-c", prefix + "default_tools_approval_mode=\"approve\"",
	}
	if env := strings.TrimSpace(mcp.BearerTokenEnvVar); env != "" {
		args = append(args, "-c", prefix+"bearer_token_env_var="+strconv.Quote(env))
	}
	if len(mcp.HTTPHeaders) > 0 {
		args = append(args, "-c", prefix+"http_headers="+tomlStringMap(mcp.HTTPHeaders))
	}
	return args, nil
}

func tomlStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Quote(key)+" = "+strconv.Quote(values[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
