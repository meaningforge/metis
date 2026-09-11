package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	piadapter "github.com/meaningforge/metis/cmd/s2sbench/bench/agent/pi"
	duckdbfixture "github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	enginefixture "github.com/meaningforge/metis/cmd/s2sbench/bench/enginefixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/lifecycle"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
	"github.com/spf13/cobra"
)

var benchTargets = map[string]readiness.Target{
	"duckdb": readiness.DuckDBTarget,
}

var benchCatalogBuilders = map[string]func(readiness.Manifest) (readiness.SourceCatalog, error){
	"duckdb": buildDuckDBFixtureCatalog,
}

type benchCLIOptions struct {
	agentName, model, provider, modelVersion, suiteValue, suitePath, armValue, engineValue, outputDir *string
	projectContext, codexBin, claudeBin, piBin, piDriver, genericBin, genericName, genericVersion     *string
	resume                                                                                            *bool
	detach, internalDetachedChild                                                                     *bool
	readinessFD                                                                                       *int
	mcpPort, piTimeoutMS                                                                              *int
	genericArgs, scenarioNames                                                                        stringListFlag
	notifier                                                                                          *lifecycle.Notifier
}

func newRunCommand(stdout io.Writer) *cobra.Command {
	options := benchCLIOptions{}
	command := &cobra.Command{Use: "run", Short: "Run one independently resumable experiment arm", Args: cobra.NoArgs}
	flags := command.Flags()
	options.agentName = flags.String("agent", "", "installed agent: codex, claude-code, pi, or generic")
	options.model = flags.String("model", "", "exact model ID used by the installed agent")
	options.provider = flags.String("provider", "", "model provider recorded in the manifest")
	options.modelVersion = flags.String("model-version", "", "exact model version/revision recorded in the manifest")
	options.suiteValue = flags.String("suite", "", "scenario suite: smoke, diagnostic, formal, cumulative-case, or filters-case")
	options.suitePath = flags.String("suite-path", "", "generated self-contained workload directory")
	options.armValue = flags.String("arm", "", "single semantic-interface arm: okf or metis-mcp")
	options.engineValue = flags.String("engine", "duckdb", "registered physical execution target")
	options.outputDir = flags.String("output", "", "new result directory; generated under ./s2sbench-results when omitted")
	options.resume = flags.Bool("resume", false, "resume the matching single-arm .partial journal without rerunning completed questions")
	options.detach = flags.Bool("detach", false, "run in a detached session and return after startup is ready")
	options.internalDetachedChild = flags.Bool("internal-detached-child", false, "internal detached child mode")
	options.readinessFD = flags.Int("readiness-fd", 0, "internal detached readiness descriptor")
	_ = flags.MarkHidden("internal-detached-child")
	_ = flags.MarkHidden("readiness-fd")
	options.mcpPort = flags.Int("mcp-port", 0, "loopback Metis MCP port; 0 allocates one port")
	options.projectContext = flags.String("project-context", string(s2sbench.ProjectContextConfigured), "Metis project context: configured or cold_start")
	options.codexBin = flags.String("codex-bin", "codex", "Codex CLI binary")
	options.claudeBin = flags.String("claude-bin", "claude", "Claude Code CLI binary")
	options.piBin = flags.String("pi-bin", "pi", "Pi CLI binary")
	options.piDriver = flags.String("pi-driver", "", "custom Pi S2SBench wrapper; embedded adapter is used by default")
	options.piTimeoutMS = flags.Int("pi-timeout-ms", 60_000, "Pi turn timeout in milliseconds")
	options.genericBin = flags.String("generic-bin", "", "generic s2sbench-driver-v2 streaming wrapper binary")
	options.genericName = flags.String("generic-name", "", "generic agent identity name")
	options.genericVersion = flags.String("generic-version", "", "generic agent identity version")
	flags.Var(&options.genericArgs, "generic-arg", "argument passed to the generic wrapper; repeatable")
	flags.Var(&options.scenarioNames, "scenario", "scenario from the selected suite; repeatable")
	for _, name := range []string{"agent", "arm", "model", "provider", "model-version"} {
		_ = command.MarkFlagRequired(name)
	}
	command.RunE = func(*cobra.Command, []string) error {
		if strings.TrimSpace(*options.outputDir) == "" {
			output, err := defaultOutputPath()
			if err != nil {
				return err
			}
			*options.outputDir = output
		}
		if *options.detach && !*options.internalDetachedChild {
			paths, err := lifecycle.ResolvePaths(*options.outputDir)
			if err != nil {
				return err
			}
			*options.outputDir = paths.Output
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			args := append(append([]string(nil), os.Args[1:]...), "--output="+paths.Output, "--internal-detached-child", "--readiness-fd=3")
			info, err := lifecycle.SpawnDetached(ctx, args, *options.outputDir, *options.resume, lifecycle.DefaultStartupTimeout)
			if errors.Is(err, lifecycle.ErrRecoveredCompletion) {
				fmt.Fprintf(stdout, "S2SBench recovered completed publication at %s\n", paths.Output)
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "S2SBench detached run is ready.\nRun directory: %s\nLog: %s\nPID metadata: %s\nStop: s2sbench stop %q\n", info.Paths.Output, info.Paths.Log, info.Paths.PID, info.Paths.Output)
			return nil
		}
		if *options.internalDetachedChild {
			notifier, err := lifecycle.NewNotifier(*options.readinessFD)
			if err != nil {
				return err
			}
			options.notifier = notifier
			defer notifier.Close()
		}
		return runBench(options, stdout)
	}
	return command
}

func runBench(options benchCLIOptions, stdout io.Writer) (runErr error) {
	readyNotified := false
	defer func() {
		if options.notifier != nil && !readyNotified && runErr != nil {
			_ = options.notifier.Failed(runErr)
		}
	}()
	agentName, model, provider, modelVersion := options.agentName, options.model, options.provider, options.modelVersion
	suiteValue, suitePath, armValue, engineValue := options.suiteValue, options.suitePath, options.armValue, options.engineValue
	outputDir, resume, mcpPort, projectContext := options.outputDir, options.resume, options.mcpPort, options.projectContext
	codexBin, claudeBin, piBin, piDriver, piTimeoutMS := options.codexBin, options.claudeBin, options.piBin, options.piDriver, options.piTimeoutMS
	genericBin, genericName, genericVersion := options.genericBin, options.genericName, options.genericVersion
	genericArgs, scenarioNames := options.genericArgs, options.scenarioNames
	if strings.TrimSpace(*agentName) == "" || strings.TrimSpace(*model) == "" || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*modelVersion) == "" || strings.TrimSpace(*armValue) == "" || strings.TrimSpace(*outputDir) == "" {
		return fmt.Errorf("run requires --arm, --agent, --model, --provider, --model-version, and one of --suite or --suite-path")
	}
	if (strings.TrimSpace(*suiteValue) == "") == (strings.TrimSpace(*suitePath) == "") {
		return fmt.Errorf("run requires exactly one of --suite or --suite-path")
	}
	var err error
	var suite readiness.Suite
	var generated *workload.Bundle
	var generatedRoot string
	if strings.TrimSpace(*suitePath) != "" {
		generatedRoot, err = filepath.Abs(*suitePath)
		if err != nil {
			return err
		}
		loaded, loadErr := workload.Load(generatedRoot)
		if loadErr != nil {
			return loadErr
		}
		generated = &loaded
		suite = readiness.Suite(loaded.Name)
	} else {
		suite, err = readiness.ParseSuite(*suiteValue)
		if err != nil {
			return err
		}
	}
	arm, err := readiness.ParseArm(*armValue)
	if err != nil {
		return err
	}
	if err := validateAgentProvider(*agentName, *provider); err != nil {
		return err
	}
	target, ok := benchTargets[strings.ToLower(strings.TrimSpace(*engineValue))]
	if !ok {
		return fmt.Errorf("engine %q is not registered", *engineValue)
	}
	absoluteOutput, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	paths, err := lifecycle.ResolvePaths(absoluteOutput)
	if err != nil {
		return err
	}
	absoluteOutput = paths.Output
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	resolvedAgentName := strings.TrimSpace(*agentName)
	cleanupPiDriver := func() {}
	defer func() { cleanupPiDriver() }()
	if resolvedAgentName == "pi" {
		if *piTimeoutMS <= 0 {
			return fmt.Errorf("--pi-timeout-ms must be positive")
		}
		versionOutput, versionErr := exec.Command(*piBin, "--version").Output()
		if versionErr != nil {
			return fmt.Errorf("resolve Pi version: %w", versionErr)
		}
		resolvedAgentName = "generic"
		if strings.TrimSpace(*piDriver) == "" {
			var materializeErr error
			*genericBin, cleanupPiDriver, materializeErr = piadapter.Materialize()
			if materializeErr != nil {
				return materializeErr
			}
		} else {
			*genericBin = *piDriver
		}
		*genericName = "pi"
		*genericVersion = strings.TrimSpace(string(versionOutput))
		piEnvironment := map[string]string{
			"S2SBENCH_PI_BIN": *piBin, "S2SBENCH_PI_PROVIDER": *provider,
			"S2SBENCH_PI_MODEL": *model, "S2SBENCH_PI_TIMEOUT_MS": fmt.Sprintf("%d", *piTimeoutMS),
		}
		if authFile := configuredPiAuthFile(); authFile != "" {
			piEnvironment["S2SBENCH_PI_AUTH_FILE"] = authFile
		}
		for name, value := range piEnvironment {
			if err := os.Setenv(name, value); err != nil {
				return fmt.Errorf("configure Pi driver environment: %w", err)
			}
		}
	}
	driver, err := buildAgentDriver(resolvedAgentName, *model, *codexBin, *claudeBin, *genericBin, *genericName, *genericVersion, genericArgs)
	if err != nil {
		return err
	}
	identity, err := driver.Identity(ctx)
	if err != nil {
		return fmt.Errorf("resolve installed agent identity: %w", err)
	}
	var definitions []fixtures.Definition
	var manifest readiness.Manifest
	var projection *readiness.Projection
	if generated != nil {
		if generated.Engine != target.Engine {
			return fmt.Errorf("workload engine %q does not match requested engine %q", generated.Engine, target.Engine)
		}
		definitions, err = generated.Definitions(generatedRoot)
		if err != nil {
			return err
		}
		projection, err = loadGeneratedProjection(generatedRoot, *generated, arm)
		if err != nil {
			return err
		}
		specs := make([]readiness.ScenarioSpec, 0, len(generated.Cases))
		for _, item := range generated.Cases {
			q, oracle := item.Query, item.Oracle
			specs = append(specs, readiness.ScenarioSpec{Name: item.Name, Stratum: s2sbench.Stratum(item.Stratum), Question: item.Question, Query: &q, ExpectedResult: &oracle})
		}
		manifest, err = readiness.NewWorkloadManifest(suite, generated.Digest, specs, arm, scenarioNames, identity, *provider, *model, *modelVersion, target)
		if generated.Knowledge.Format != "" {
			manifest.ProjectionVersion = generated.Knowledge.ProjectionVersion
			manifest.OKFVersion = generated.Knowledge.Version
			manifest.OKFRevision = generated.Knowledge.Revision
		}
		manifest.MaterialSource = generated.Source
		manifest.MaterialDigest = generated.Digest
	} else {
		manifest, err = readiness.NewManifestForTarget(suite, arm, scenarioNames, identity, *provider, *model, *modelVersion, target)
		if err != nil {
			return err
		}
		definitions, err = fixtures.CanonicalSemanticModels()
		if err != nil {
			return fmt.Errorf("load canonical semantic project: %w", err)
		}
		buildCatalog, ok := benchCatalogBuilders[target.Engine]
		if !ok {
			return fmt.Errorf("engine %q has no source-catalog adapter for the OKF arm", target.Engine)
		}
		catalog, catalogErr := buildCatalog(manifest)
		if catalogErr != nil {
			return catalogErr
		}
		projection, err = readiness.BuildCatalogProjection(catalog)
		if err != nil {
			return fmt.Errorf("build audited OKF catalog projection: %w", err)
		}
		manifest.ProjectionVersion = projection.Ledger.ProjectionVersion
	}
	if err != nil {
		return err
	}
	manifest.ProjectContextMode = s2sbench.ProjectContextMode(strings.TrimSpace(*projectContext))
	if err := manifest.Validate(); err != nil {
		return err
	}
	fixtureIdentity := make([]enginefixture.Dataset, 0)
	if generated == nil {
		resolvedScenarios, resolveErr := readiness.ResolveScenarios(manifest)
		if resolveErr != nil {
			return resolveErr
		}
		for _, scenario := range resolvedScenarios {
			dataset, ok := enginefixture.Lookup(scenario.Fixture)
			if !ok {
				return fmt.Errorf("scenario %q references unknown fixture %q", scenario.Name, scenario.Fixture)
			}
			fixtureIdentity = append(fixtureIdentity, dataset)
		}
	}
	compatibility, err := lifecycle.CompatibilityDigest(struct {
		Manifest     readiness.Manifest
		LedgerDigest string
		Definitions  []fixtures.Definition
		Fixtures     []enginefixture.Dataset
	}{manifest, projection.Ledger.Digest, definitions, fixtureIdentity})
	if err != nil {
		return err
	}
	begin := lifecycle.BeginCompatible
	if *options.internalDetachedChild {
		begin = lifecycle.BeginPreparedCompatible
	}
	lease, err := begin(absoluteOutput, *resume, compatibility)
	if errors.Is(err, lifecycle.ErrRecoveredCompletion) {
		fmt.Fprintf(stdout, "S2SBench recovered completed publication at %s\n", absoluteOutput)
		return nil
	}
	if err != nil {
		return err
	}
	defer lease.Close()
	var journal *readinessJournal
	var resumedRecords []readiness.AttemptRecord
	if *resume {
		journal, resumedRecords, err = resumeReadinessJournal(absoluteOutput, manifest, projection.Ledger)
	} else {
		journal, err = newReadinessJournal(absoluteOutput, manifest, projection.Ledger)
	}
	if err != nil {
		return err
	}
	defer journal.Close()
	observer := func(record readiness.AttemptRecord) error {
		if err := journal.Append(record); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s/%s attempt=%d verdict=%s tools=%d duration_ms=%d\n", record.Arm, record.Scenario, record.Attempt, record.Verdict, record.ToolCalls, record.DurationMS)
		return nil
	}

	runtimeRoot, err := os.MkdirTemp("", "metis-s2sbench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(runtimeRoot)
	databasePath := filepath.Join(runtimeRoot, "s2sbench.duckdb")
	var execution readiness.Execution
	var closeExecution func() error
	if generated != nil {
		databasePath, err = workload.ResolveAsset(generatedRoot, generated.Database.Path)
		if err != nil {
			return err
		}
		execution, closeExecution, err = newBundleDuckDBExecution(generatedRoot, *generated)
	} else {
		duckdb, backendErr := duckdbfixture.New(databasePath)
		err = backendErr
		execution = duckdb
		closeExecution = func() error { return duckdb.Close(context.Background()) }
	}
	if err != nil {
		return err
	}
	defer closeExecution()
	var decorate readiness.RequestDecorator
	var decorateAttempt readiness.AttemptDecorator
	if arm == readiness.ArmMetisMCP {
		runtime, runtimeErr := newExecutableSemanticProjectRuntime(filepath.Join(runtimeRoot, "semantic-project"), *mcpPort, definitions, databasePath)
		if runtimeErr != nil {
			return runtimeErr
		}
		defer runtime.Close(context.Background())
		execution = &metisExecution{base: execution, runtime: runtime}
		decorate = func(_ context.Context, _ readiness.Arm, request s2sbench.AgentRequest) (s2sbench.AgentRequest, error) {
			mcp, _, environment, endpointErr := runtime.CurrentEndpoint()
			if endpointErr != nil {
				return s2sbench.AgentRequest{}, endpointErr
			}
			if manifest.ProjectContextMode == s2sbench.ProjectContextColdStart {
				mcp.HTTPHeaders = nil
			}
			request.MCP = &mcp
			request.Environment = environment
			return request, nil
		}
		decorateAttempt = runtime.captures.decorateAttempt
	}
	if err := lease.Ready(); err != nil {
		return err
	}
	if options.notifier != nil {
		if err := options.notifier.Ready(lease.Manifest()); err != nil {
			return fmt.Errorf("acknowledge detached readiness: %w", err)
		}
		readyNotified = true
	}
	fmt.Fprintf(stdout, "S2SBench %s/%s started; incremental evidence: %s\n", suite, arm, filepath.Join(journal.partial, "attempts.jsonl"))
	collection, report, err := readiness.CollectFromWithDecorators(ctx, manifest, driver, execution, projection, definitions, resumedRecords, observer, decorate, decorateAttempt)
	if err != nil {
		return fmt.Errorf("S2SBench %s/%s failed; partial evidence retained at %s: %w", suite, arm, journal.partial, err)
	}
	if err := lease.Publishing(); err != nil {
		return err
	}
	if err := journal.Commit(collection, report); err != nil {
		return err
	}
	if err := lease.Complete(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "S2SBench %s/%s complete: first_ready=%d rate=%.2f valid=%t\n", suite, arm, report.FirstReady, report.ReadinessRate, report.Valid)
	fmt.Fprintf(stdout, "S2SBench evidence written to %s\n", absoluteOutput)
	return nil
}

func defaultOutputPath() (string, error) {
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate output identity: %w", err)
	}
	name := fmt.Sprintf("run-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(nonce[:]))
	return filepath.Abs(filepath.Join("s2sbench-results", name))
}

// configuredPiAuthFile resolves only Pi's credential file from the user's
// configured Pi directory. The generic driver still gives the Agent an
// isolated HOME; the embedded Pi adapter copies this one file into it.
func configuredPiAuthFile() string {
	directory := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		directory = filepath.Join(home, ".pi", "agent")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return ""
	}
	path := filepath.Join(directory, "auth.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func buildDuckDBFixtureCatalog(manifest readiness.Manifest) (readiness.SourceCatalog, error) {
	resolved, err := readiness.ResolveScenarios(manifest)
	if err != nil {
		return readiness.SourceCatalog{}, err
	}
	byName := map[string]readiness.CatalogTable{}
	for _, scenario := range resolved {
		dataset, ok := enginefixture.Lookup(scenario.Fixture)
		if !ok {
			return readiness.SourceCatalog{}, fmt.Errorf("scenario %q references unknown fixture %q", scenario.Name, scenario.Fixture)
		}
		for _, sourceTable := range dataset.Tables {
			table, exists := byName[sourceTable.Name]
			if !exists {
				table = readiness.CatalogTable{Name: sourceTable.Name}
				for index, column := range sourceTable.Columns {
					physicalType, typeErr := duckDBCatalogType(column.Type)
					if typeErr != nil {
						return readiness.SourceCatalog{}, typeErr
					}
					table.Columns = append(table.Columns, readiness.CatalogColumn{Position: index + 1, Name: column.Name, Type: physicalType, Nullable: column.Nullable})
				}
			}
			if len(sourceTable.Rows) > int(table.RowCount) {
				table.RowCount = int64(len(sourceTable.Rows))
			}
			if len(table.Columns) != len(sourceTable.Columns) {
				return readiness.SourceCatalog{}, fmt.Errorf("fixture table %q has incompatible schema variants", sourceTable.Name)
			}
			for index, column := range sourceTable.Columns {
				physicalType, typeErr := duckDBCatalogType(column.Type)
				if typeErr != nil {
					return readiness.SourceCatalog{}, typeErr
				}
				if table.Columns[index].Name != column.Name || table.Columns[index].Type != physicalType {
					return readiness.SourceCatalog{}, fmt.Errorf("fixture table %q has incompatible schema variants", sourceTable.Name)
				}
				table.Columns[index].Nullable = table.Columns[index].Nullable || column.Nullable
			}
			byName[table.Name] = table
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	catalog := readiness.SourceCatalog{
		SchemaVersion: readiness.CatalogSchemaVersion, Engine: "duckdb", EngineTitle: "DuckDB",
		Database: "s2sbench", Schema: "analytics", Specification: "S2SBench canonical fixture catalog",
		Generator: "S2SBench fixture registry", GeneratedAt: "2026-09-02T00:00:00Z", Tags: []string{"s2sbench-fixture"},
	}
	for _, name := range names {
		catalog.Tables = append(catalog.Tables, byName[name])
	}
	return catalog, catalog.Validate()
}

func duckDBCatalogType(value enginefixture.LogicalType) (string, error) {
	switch value {
	case enginefixture.String:
		return "VARCHAR", nil
	case enginefixture.Integer:
		return "BIGINT", nil
	case enginefixture.Float:
		return "DOUBLE", nil
	case enginefixture.Decimal:
		return "DECIMAL(20,12)", nil
	case enginefixture.Boolean:
		return "BOOLEAN", nil
	case enginefixture.Date:
		return "DATE", nil
	case enginefixture.DateTime:
		return "TIMESTAMP", nil
	default:
		return "", fmt.Errorf("unsupported DuckDB fixture type %q", value)
	}
}

func loadGeneratedOKFProjection(root string, bundle workload.Bundle) (*readiness.Projection, error) {
	if bundle.Knowledge.Format == "" {
		return nil, fmt.Errorf("generated workload has no frozen OKF knowledge; run s2sbench okfgen --input %q first", root)
	}
	if bundle.Knowledge.Format != "okf" || bundle.Knowledge.Version != readiness.OKFVersion || bundle.Knowledge.Revision != readiness.OKFRevision || bundle.Knowledge.ProjectionVersion != readiness.CatalogProjectionVersion {
		return nil, fmt.Errorf("generated workload OKF identity is unsupported")
	}
	catalogPath, err := workload.ResolveAsset(root, bundle.Knowledge.Catalog.Path)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, err
	}
	catalog, err := readiness.DecodeSourceCatalog(body)
	if err != nil {
		return nil, err
	}
	projection, err := readiness.BuildCatalogProjection(catalog)
	if err != nil {
		return nil, err
	}
	if len(bundle.Knowledge.Files) != len(projection.Files) {
		return nil, fmt.Errorf("generated workload OKF file count differs from its source-catalog concept inventory")
	}
	for _, asset := range bundle.Knowledge.Files {
		relative := strings.TrimPrefix(filepath.ToSlash(asset.Path), "knowledge/")
		_, ok := projection.Files[relative]
		if !ok {
			return nil, fmt.Errorf("generated workload contains unexpected OKF asset %q", asset.Path)
		}
		path, resolveErr := workload.ResolveAsset(root, asset.Path)
		if resolveErr != nil {
			return nil, resolveErr
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		projection.Files[relative] = got
	}
	if err := projection.Validate(); err != nil {
		return nil, fmt.Errorf("validate frozen generated OKF bundle: %w", err)
	}
	return projection, nil
}

func loadGeneratedProjection(root string, bundle workload.Bundle, arm readiness.Arm) (*readiness.Projection, error) {
	if bundle.Knowledge.Format != "" {
		return loadGeneratedOKFProjection(root, bundle)
	}
	if arm != readiness.ArmMetisMCP {
		return nil, fmt.Errorf("generated workload has no frozen OKF knowledge; run s2sbench okfgen --input %q first", root)
	}
	catalogPath, err := workload.ResolveAsset(root, "catalog/catalog.json")
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("read generated source catalog: %w", err)
	}
	catalog, err := readiness.DecodeSourceCatalog(body)
	if err != nil {
		return nil, err
	}
	return readiness.BuildCatalogProjection(catalog)
}
