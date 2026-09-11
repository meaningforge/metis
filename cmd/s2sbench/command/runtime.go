package command

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/meaningforge/metis/app/httperr"
	appmcp "github.com/meaningforge/metis/app/mcp"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

const s2sbenchMCPTokenEnv = "METIS_S2SBENCH_MCP_TOKEN"
const s2sbenchDataSource = "s2sbench-duckdb"

// semanticProjectRuntime is the one immutable semantic world shared by every
// Metis-arm question. Scenario lifecycle never reloads or narrows this runtime;
// only the DuckDB physical fixture changes between questions.
type semanticProjectRuntime struct {
	root        string
	project     string
	server      *http.Server
	listener    net.Listener
	endpoint    s2sbench.AgentMCP
	environment map[string]string
	execution   *runner.Runner
	captures    *physicalQueryCapture

	closeOnce sync.Once
	closeErr  error
}

func newSemanticProjectRuntime(root string, port int, models []fixtures.Definition) (*semanticProjectRuntime, error) {
	return newSemanticProjectRuntimeWithDatabase(root, port, models, "")
}

func newExecutableSemanticProjectRuntime(root string, port int, models []fixtures.Definition, database string) (*semanticProjectRuntime, error) {
	return newSemanticProjectRuntimeWithDatabase(root, port, models, database)
}

func newSemanticProjectRuntimeWithDatabase(root string, port int, models []fixtures.Definition, database string) (*semanticProjectRuntime, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("semantic project runtime root is required")
	}
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("MCP port must be between 0 and 65535")
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("canonical semantic project requires at least one model")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create semantic project runtime root: %w", err)
	}

	address := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("reserve benchmark MCP listener on %s: %w", address, err)
	}
	cleanupListener := true
	defer func() {
		if cleanupListener {
			_ = listener.Close()
		}
	}()

	projectPath, project, err := writeSemanticProject(root, models)
	if err != nil {
		return nil, err
	}
	captures := newPhysicalQueryCapture()
	projectRuntime, err := loadSemanticProjectRuntime(projectPath, project, database, captures)
	if err != nil {
		return nil, fmt.Errorf("load canonical multi-model Metis runtime: %w", err)
	}
	token, err := randomBearerToken()
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: bearerAuth(token, appmcp.NewObservedHTTPHandlerWithDimensionValues(projectRuntime.Discovery, projectRuntime.Compile, projectRuntime.QueryMetrics, projectRuntime.DimensionValues, projectRuntime.AttributeMetric, projectRuntime.CompareMetrics, nil, nil))}
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
		close(serveErr)
	}()
	select {
	case err := <-serveErr:
		if err == nil {
			return nil, fmt.Errorf("benchmark MCP server stopped before use")
		}
		return nil, fmt.Errorf("serve benchmark MCP: %w", err)
	default:
	}

	cleanupListener = false
	return &semanticProjectRuntime{
		root:     root,
		project:  project,
		server:   server,
		listener: listener,
		endpoint: s2sbench.AgentMCP{
			Name:              "metis",
			URL:               "http://" + listener.Addr().String(),
			BearerTokenEnvVar: s2sbenchMCPTokenEnv,
			HTTPHeaders:       map[string]string{appmcp.ProjectHeaderKey: project},
		},
		environment: map[string]string{s2sbenchMCPTokenEnv: token},
		execution:   projectRuntime.Execution,
		captures:    captures,
	}, nil
}

func writeSemanticProject(root string, models []fixtures.Definition) (string, string, error) {
	modelsDir := filepath.Join(root, "models")
	if err := os.MkdirAll(modelsDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create semantic project models directory: %w", err)
	}
	project := strings.TrimSpace(models[0].Project)
	if project == "" {
		return "", "", fmt.Errorf("canonical semantic model project is required")
	}
	seen := map[string]struct{}{}
	var projectConfig strings.Builder
	projectConfig.WriteString("semantic_sources:\n")
	for _, definition := range models {
		if strings.TrimSpace(definition.Project) != project {
			return "", "", fmt.Errorf("canonical semantic models span projects %q and %q", project, definition.Project)
		}
		model := strings.TrimSpace(definition.Model)
		if model == "" || len(definition.Document) == 0 {
			return "", "", fmt.Errorf("canonical semantic model identity/document is incomplete")
		}
		if _, exists := seen[model]; exists {
			return "", "", fmt.Errorf("duplicate canonical semantic model %q", model)
		}
		seen[model] = struct{}{}
		name := sanitizeWorkspaceComponent(model) + ".ossie.yaml"
		if err := os.WriteFile(filepath.Join(modelsDir, name), definition.Document, 0o600); err != nil {
			return "", "", fmt.Errorf("write canonical semantic model %q: %w", model, err)
		}
		fmt.Fprintf(&projectConfig, "  %s:\n    path: ./models/%s\n", sanitizeWorkspaceComponent(model), name)
	}
	if err := os.WriteFile(filepath.Join(root, "project.yaml"), []byte(projectConfig.String()), 0o600); err != nil {
		return "", "", fmt.Errorf("write canonical semantic project config: %w", err)
	}
	rootConfig := fmt.Sprintf("version: 1\nprojects:\n  %q:\n    path: ./project.yaml\n", project)
	projectPath := filepath.Join(root, "metis.yaml")
	if err := os.WriteFile(projectPath, []byte(rootConfig), 0o600); err != nil {
		return "", "", fmt.Errorf("write canonical deployment config: %w", err)
	}
	return projectPath, project, nil
}

func (r *semanticProjectRuntime) CurrentEndpoint() (s2sbench.AgentMCP, string, map[string]string, error) {
	if r == nil || r.server == nil || r.listener == nil || r.endpoint.URL == "" || r.project == "" {
		return s2sbench.AgentMCP{}, "", nil, fmt.Errorf("Metis semantic project runtime is not available")
	}
	environment := make(map[string]string, len(r.environment))
	for name, value := range r.environment {
		environment[name] = value
	}
	headers := make(map[string]string, len(r.endpoint.HTTPHeaders))
	for name, value := range r.endpoint.HTTPHeaders {
		headers[name] = value
	}
	endpoint := r.endpoint
	endpoint.HTTPHeaders = headers
	return endpoint, r.project, environment, nil
}

func (r *semanticProjectRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.server != nil {
			if err := r.server.Shutdown(ctx); err != nil {
				r.closeErr = fmt.Errorf("shutdown benchmark MCP server: %w", err)
			}
		}
		if r.execution != nil {
			if err := r.execution.Close(ctx); err != nil {
				r.closeErr = errors.Join(r.closeErr, fmt.Errorf("close benchmark execution runtime: %w", err))
			}
		}
		if r.listener != nil {
			_ = r.listener.Close()
		}
	})
	return r.closeErr
}

type metisExecution struct {
	duckdb  *fixture.Backend
	base    readiness.Execution
	runtime *semanticProjectRuntime
}

func (e *metisExecution) execution() readiness.Execution {
	if e.base != nil {
		return e.base
	}
	return e.duckdb
}

func (e *metisExecution) PhysicalSchema(ctx context.Context) (string, error) {
	if e == nil || e.execution() == nil {
		return "", fmt.Errorf("Metis benchmark execution is not configured")
	}
	return e.execution().PhysicalSchema(ctx)
}

func (e *metisExecution) Prepare(ctx context.Context, scenario scenarios.Scenario) error {
	if e == nil || e.execution() == nil {
		return fmt.Errorf("Metis benchmark execution is not configured")
	}
	if e.runtime != nil && e.runtime.execution != nil {
		if err := e.runtime.execution.RecycleDataSource(ctx, s2sbenchDataSource); err != nil {
			return fmt.Errorf("recycle benchmark DataSource before fixture reset: %w", err)
		}
	}
	return e.execution().Prepare(ctx, scenario)
}

func (e *metisExecution) RunSQL(ctx context.Context, sql string, params ...sql.QueryParameter) (scenarios.ResultSet, error) {
	if e == nil || e.execution() == nil {
		return scenarios.ResultSet{}, fmt.Errorf("Metis benchmark execution is not configured")
	}
	return e.execution().RunSQL(ctx, sql, params...)
}

func randomBearerToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate benchmark MCP bearer token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		if len(got) != len(token) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", httperr.StatusOf(serrors.ErrUnauthenticated))
			return
		}
		next.ServeHTTP(w, req)
	})
}
