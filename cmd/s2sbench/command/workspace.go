package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

type physicalSchemaProvider interface {
	PhysicalSchema(ctx context.Context) (string, error)
}

type semanticEndpointProvider interface {
	CurrentEndpoint() (mcp s2sbench.AgentMCP, project string, environment map[string]string, err error)
}

type workspaceFactory struct {
	semanticProject    string
	metisWorkspaceRoot string
	metisWorkspace     string
	schema             physicalSchemaProvider
	semantic           semanticEndpointProvider
	bindProjectContext bool
}

func newWorkspaceFactory(semanticProject, metisWorkspaceRoot string, schema physicalSchemaProvider, semantic semanticEndpointProvider) (*workspaceFactory, error) {
	semanticProject = strings.TrimSpace(semanticProject)
	if semanticProject == "" {
		return nil, fmt.Errorf("semantic project directory is required")
	}
	if schema == nil {
		return nil, fmt.Errorf("physical schema provider is required")
	}
	if semantic == nil {
		return nil, fmt.Errorf("semantic endpoint provider is required")
	}
	info, err := os.Stat(semanticProject)
	if err != nil {
		return nil, fmt.Errorf("inspect semantic project directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("semantic project path %q is not a directory", semanticProject)
	}
	metisWorkspaceRoot = strings.TrimSpace(metisWorkspaceRoot)
	if metisWorkspaceRoot == "" {
		return nil, fmt.Errorf("Metis Agent workspace root is required")
	}
	info, err = os.Stat(metisWorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect Metis Agent workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Metis Agent workspace root %q is not a directory", metisWorkspaceRoot)
	}
	return &workspaceFactory{
		semanticProject:    semanticProject,
		metisWorkspaceRoot: metisWorkspaceRoot,
		schema:             schema,
		semantic:           semantic,
		bindProjectContext: true,
	}, nil
}

func (f *workspaceFactory) WithBoundProjectContext(bound bool) *workspaceFactory {
	if f != nil {
		f.bindProjectContext = bound
	}
	return f
}

func (f *workspaceFactory) BeginSession(_ int) error {
	if f == nil {
		return fmt.Errorf("workspace factory is required")
	}
	// A same-question repair reuses its workspace, but every independently scored
	// question starts without notes, scripts, caches, or semantic source files.
	f.metisWorkspace = ""
	return nil
}

func (f *workspaceFactory) BuildAgentRequest(ctx context.Context, path s2sbench.Path, question string, scenario scenarios.Scenario, budget s2sbench.TaskBudget, failure string) (s2sbench.AgentRequest, error) {
	if f == nil {
		return s2sbench.AgentRequest{}, fmt.Errorf("workspace factory is required")
	}
	firstTurn := failure == ""

	switch path {
	case s2sbench.PathRawAssets:
		if firstTurn {
			physicalSchema, err := f.schema.PhysicalSchema(ctx)
			if err != nil {
				return s2sbench.AgentRequest{}, fmt.Errorf("load current physical schema: %w", err)
			}
			if strings.TrimSpace(physicalSchema) == "" {
				return s2sbench.AgentRequest{}, fmt.Errorf("current physical schema is empty")
			}
			schemaPath := filepath.Join(f.semanticProject, "schema.sql")
			if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
				if err := writeReadonlyFile(schemaPath, []byte(physicalSchema)); err != nil {
					return s2sbench.AgentRequest{}, err
				}
			} else if err != nil {
				return s2sbench.AgentRequest{}, fmt.Errorf("inspect benchmark schema: %w", err)
			} else if err := replaceReadonlyFile(schemaPath, []byte(physicalSchema)); err != nil {
				return s2sbench.AgentRequest{}, err
			}
		}
		return s2sbench.AgentRequest{
			Workspace: f.semanticProject,
			Prompt:    withAttemptToolBudget(s2sbench.RawAgentTurnPrompt(question, failure, firstTurn), budget),
			Budget:    budget,
		}, nil

	case s2sbench.PathMetis:
		if firstTurn {
			workspace, err := os.MkdirTemp(f.metisWorkspaceRoot, "question-")
			if err != nil {
				return s2sbench.AgentRequest{}, fmt.Errorf("create clean Metis Agent workspace: %w", err)
			}
			f.metisWorkspace = workspace
		} else if f.metisWorkspace == "" {
			return s2sbench.AgentRequest{}, fmt.Errorf("Metis repair has no active question workspace")
		}
		mcp, project, environment, err := f.semantic.CurrentEndpoint()
		if err != nil {
			return s2sbench.AgentRequest{}, err
		}
		if strings.TrimSpace(mcp.Name) == "" || strings.TrimSpace(mcp.URL) == "" || strings.TrimSpace(project) == "" {
			return s2sbench.AgentRequest{}, fmt.Errorf("Metis MCP endpoint and project namespace are required for path B")
		}
		promptProject := ""
		if !f.bindProjectContext {
			mcp.HTTPHeaders = nil
			promptProject = project
		}
		return s2sbench.AgentRequest{
			Workspace:   f.metisWorkspace,
			Prompt:      withAttemptToolBudget(s2sbench.MetisAgentTurnPrompt(question, promptProject, failure, firstTurn), budget),
			MCP:         &mcp,
			Environment: environment,
			Budget:      budget,
		}, nil
	default:
		return s2sbench.AgentRequest{}, fmt.Errorf("unknown benchmark path %q", path)
	}
}

func withAttemptToolBudget(prompt string, budget s2sbench.TaskBudget) string {
	return fmt.Sprintf("%s\n\nYou may make at most %d tool calls in this turn. Any repair receives only the remaining shared tool-call budget. The complete question has a %d-second wall-clock limit; any repair shares the remaining time.", prompt, budget.ToolCalls, budget.QuestionTimeoutMS/1000)
}

func sanitizeWorkspaceComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return out.String()
}

func writeReadonlyFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o400); err != nil {
		return fmt.Errorf("write benchmark asset %q: %w", path, err)
	}
	return nil
}

func replaceReadonlyFile(path string, body []byte) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("make benchmark asset writable %q: %w", path, err)
	}
	if err := os.WriteFile(path, body, 0o400); err != nil {
		return fmt.Errorf("replace benchmark asset %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		return fmt.Errorf("make benchmark asset read-only %q: %w", path, err)
	}
	return nil
}
