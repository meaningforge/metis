package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/hosting"
	"github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/app/service/runtime"
	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/version"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	configureLogging(commandLogWriter(os.Args))
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "model", "project", "query":
		os.Exit(runOffline(os.Args[1:]))
	case "validate":
		validate(os.Args[2:])
	case "inspect":
		inspect(os.Args[2:])
	case "serve":
		serve(os.Args[2:])
	case "mcp":
		mcpCommand(os.Args[2:])
	case "version":
		printVersion()
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func commandLogWriter(args []string) io.Writer {
	if len(args) > 1 && (args[1] == "mcp" || args[1] == "model" || args[1] == "project" || args[1] == "query") {
		return os.Stderr
	}
	return os.Stdout
}

func configureLogging(writer io.Writer) {
	level := slog.LevelInfo
	if os.Getenv("METIS_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})))
}

func validate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	file := fs.String("file", "", "path to Ossie YAML/JSON")
	project := fs.String("project", "", "Metis project namespace")
	_ = fs.Parse(args)
	if *file == "" && fs.NArg() > 0 {
		*file = fs.Arg(0)
	}
	if *file == "" || *project == "" {
		fmt.Fprintln(os.Stderr, "usage: metis validate --project <name> <model.ossie.yaml>")
		os.Exit(2)
	}
	doc, err := ossie.NewLoader().LoadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	semanticManifest, err := manifest.BuildProjectManifest(*project, doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p, err := semanticManifest.Project(*project)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("valid: project=%s version=%s models=%d digest=%s\n", *project, semanticManifest.Version, len(p.Models), semanticManifest.Digest)
}

func inspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	file := fs.String("file", "", "path to Ossie YAML/JSON")
	project := fs.String("project", "", "Metis project namespace")
	model := fs.String("model", "", "semantic model name")
	_ = fs.Parse(args)
	if *file == "" || *project == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "usage: metis inspect --project <name> --file <model.ossie.yaml> --model <name>")
		os.Exit(2)
	}
	doc, err := ossie.NewLoader().LoadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	semanticManifest, err := manifest.BuildProjectManifest(*project, doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p, err := semanticManifest.Project(*project)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	idx, err := p.Model(*model)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := map[string]any{
		"project":            *project,
		"name":               idx.Model.Name,
		"description":        idx.Model.Description,
		"datasets":           len(idx.Datasets),
		"dataset_names":      sortedKeys(idx.Datasets),
		"metrics":            len(idx.Metrics),
		"metric_names":       sortedKeys(idx.Metrics),
		"relationships":      len(idx.Relationships),
		"relationship_names": sortedKeys(idx.Relationships),
		"digest":             semanticManifest.Digest,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func printVersion() {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(version.Current())
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to a Metis project or deployment manifest")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	metricsEnabled := fs.Bool("metrics", false, "enable unauthenticated operator metrics at /metrics")
	tracesEnabled := fs.Bool("otlp-traces", false, "export OpenTelemetry traces over OTLP/HTTP")
	traceSampleRatio := fs.Float64("trace-sample-ratio", 1, "root trace sampling ratio from 0 to 1")
	shutdownTimeout := fs.Duration("shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	_ = fs.Parse(args)
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: metis serve --config <metis.yaml> [--addr :8080] [--metrics] [--otlp-traces]")
		os.Exit(2)
	}

	verifier, err := auth.NewStaticAPIKeyVerifier(os.Getenv("METIS_API_KEY"))
	if err != nil {
		slog.Error("startup failed", "error", "METIS_API_KEY is required")
		os.Exit(1)
	}

	runtime, err := loadServeRuntime(*configPath)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	tracing := observability.NewTracing(nil)
	shutdownTracing := func(context.Context) error { return nil }
	if *tracesEnabled {
		tracing, shutdownTracing, err = observability.NewOTLPTracing(context.Background(), *traceSampleRatio)
		if err != nil {
			slog.Error("startup failed", "error", err)
			os.Exit(1)
		}
	}
	recorder := observability.NewRecorder()
	var metrics *observability.Metrics
	if *metricsEnabled {
		metrics = observability.NewMetrics()
		prometheusSink, registerErr := observability.NewPrometheusSink(metrics.Registerer())
		if registerErr != nil {
			slog.Error("startup failed", "error", registerErr)
			os.Exit(1)
		}
		recorder = observability.NewRecorder(prometheusSink)
	}
	configureRuntimeObservation(runtime, recorder, tracing)

	var metricsHandler http.Handler
	if metricsEnabled != nil && *metricsEnabled {
		metricsHandler = metrics.Handler()
	}
	handler, err := hosting.NewHTTPHandler(runtime, hosting.HTTPOptions{Verifier: verifier, Recorder: recorder, Tracing: tracing, Metrics: metricsHandler})
	if err != nil {
		slog.Error("HTTP assembly failed", "error", err)
		os.Exit(1)
	}

	projectIDs := runtime.ProjectIDs()
	modelCount := 0
	for _, projectID := range projectIDs {
		generation := runtime.Current(projectID)
		if generation == nil {
			continue
		}
		project, _ := generation.SemanticManifest.Project(projectID)
		if project != nil {
			modelCount += len(project.Models)
		}
	}
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	slog.Info("metis server started",
		"addr", *addr,
		"projects", projectIDs,
		"models", modelCount,
		"version", version.Version,
		"metrics_enabled", *metricsEnabled,
		"traces_enabled", *tracesEnabled,
		"trace_sample_ratio", *traceSampleRatio,
	)

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-sigCtx.Done():
		slog.Info("shutdown signal received")
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
			if runtime.Execution != nil {
				if shutdownErr := runtime.Execution.Close(shutdownCtx); shutdownErr != nil {
					slog.Error("execution runtime shutdown failed", "error", shutdownErr)
				}
			}
			_ = shutdownTracing(shutdownCtx)
			cancel()
			os.Exit(1)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		if runtime.Execution != nil {
			if shutdownErr := runtime.Execution.Close(shutdownCtx); shutdownErr != nil {
				slog.Error("execution runtime shutdown failed", "error", shutdownErr)
			}
		}
		if shutdownErr := shutdownTracing(shutdownCtx); shutdownErr != nil {
			slog.Error("trace shutdown failed", "error", shutdownErr)
		}
		cancel()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		_ = server.Close()
		if runtime.Execution != nil {
			if shutdownErr := runtime.Execution.Close(ctx); shutdownErr != nil {
				slog.Error("execution runtime shutdown failed", "error", shutdownErr)
			}
		}
		_ = shutdownTracing(ctx)
		os.Exit(1)
	}
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server stopped with error", "error", err)
			os.Exit(1)
		}
	default:
	}
	if runtime.Execution != nil {
		if shutdownErr := runtime.Execution.Close(ctx); shutdownErr != nil {
			slog.Error("execution runtime shutdown failed", "error", shutdownErr)
		}
	}
	if err := shutdownTracing(ctx); err != nil {
		slog.Error("trace shutdown failed", "error", err)
	}
	slog.Info("metis server stopped")
}

func mcpCommand(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	configPath := fs.String("config", "", "path to a Metis project or deployment manifest")
	shutdownTimeout := fs.Duration("shutdown-timeout", 10*time.Second, "runtime shutdown timeout")
	_ = fs.Parse(args)
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: metis mcp --config <metis.yaml> [--shutdown-timeout 10s]")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runStdioMCP(ctx, *configPath, *shutdownTimeout, &mcpsdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("stdio MCP server failed", "error", err)
		os.Exit(1)
	}
}

func runStdioMCP(ctx context.Context, configPath string, shutdownTimeout time.Duration, transport mcpsdk.Transport) error {
	if transport == nil {
		return errors.New("stdio MCP transport is required")
	}
	runtime, err := loadServeRuntime(configPath)
	if err != nil {
		return err
	}
	recorder := observability.NewRecorder()
	tracing := observability.NewTracing(nil)
	configureRuntimeObservation(runtime, recorder, tracing)

	server := mcp.NewObservedServerWithDimensionValues(runtime.Discovery, runtime.Compile, runtime.QueryMetrics, runtime.DimensionValues, runtime.AttributeMetric, runtime.CompareMetrics, recorder, tracing, runtime.Generations)
	principal := &auth.Principal{
		TenantID:  "local",
		SubjectID: "stdio",
		APIKeyID:  "local-process",
		Scopes:    []string{auth.ScopeAll},
	}
	ctx = auth.WithPrincipal(ctx, principal)
	ctx = runtime.Generations.Pin(ctx)
	slog.Info("stdio MCP server started", "projects", runtime.ProjectIDs(), "version", version.Version)
	runErr := server.Run(ctx, transport)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var shutdownErr error
	if runtime.Execution != nil {
		shutdownErr = runtime.Execution.Close(shutdownCtx)
	}
	slog.Info("stdio MCP server stopped")
	if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
		runErr = nil
	}
	return errors.Join(runErr, shutdownErr)
}

func configureRuntimeObservation(serveRuntime *bootstrap.Runtime, recorder *observability.Recorder, tracing *observability.Tracing) {
	serveRuntime.Generations.Configure(func(generation *runtime.Generation) {
		generation.Compile.WithObservability(recorder, tracing)
		generation.AttributeMetric.WithObservability(recorder)
		generation.CompareMetrics.WithObservability(recorder)
	})
	serveRuntime.Compile.WithObservability(recorder, tracing)
	serveRuntime.AttributeMetric.WithObservability(recorder)
	serveRuntime.CompareMetrics.WithObservability(recorder)
	if serveRuntime.Execution != nil {
		serveRuntime.Execution.WithObserver(recorder)
	}
	serveRuntime.WithProjectAuthorizationObserver(semantic.ProjectAuthorizationObserverFunc(func(ctx context.Context, audit semantic.ProjectAuthorizationAudit) {
		slog.InfoContext(ctx, "project authorization decision",
			"tenant_id", audit.TenantID,
			"subject_id", audit.SubjectID,
			"api_key_id", audit.APIKeyID,
			"project_id", audit.ProjectID,
			"action", audit.Action,
			"effect", audit.Effect,
			"reason", audit.Reason,
		)
		recorder.Record(ctx, observability.AuthorizationObservation{
			Action: observability.AuthorizationAction(audit.Action),
			Effect: observability.AuthorizationEffect(audit.Effect),
			Reason: observability.AuthorizationReason(audit.Reason),
		})
	}))
}

// loadServeRuntime is the production runtime assembly used by metis serve.
// Keeping it focused makes the linked BackendRegistry testable without starting
// an HTTP listener.
func loadServeRuntime(configPath string) (*bootstrap.Runtime, error) {
	backends, err := defaultBackends()
	if err != nil {
		return nil, err
	}
	return bootstrap.LoadRuntime(configPath,
		bootstrap.WithBackendRegistry(backends),
		bootstrap.WithSecretResolver(runner.NewEnvSecretResolver()),
		bootstrap.WithProjectAuthorizer(semantic.ScopeProjectAuthorizer{}),
	)
}

func usage() {
	fmt.Fprintln(os.Stderr, "Metis Semantic Engine")
	offlineUsage()
	fmt.Fprintln(os.Stderr, "  metis validate --project <name> <model.ossie.yaml>")
	fmt.Fprintln(os.Stderr, "  metis inspect --project <name> --file <model.ossie.yaml> --model <name>")
	fmt.Fprintln(os.Stderr, "  metis version")
	fmt.Fprintln(os.Stderr, "  metis mcp --config <metis.yaml> [--shutdown-timeout 10s]")
	fmt.Fprintln(os.Stderr, "  METIS_API_KEY=<key> metis serve --config <metis.yaml> [--addr :8080] [--metrics] [--otlp-traces]")
}
