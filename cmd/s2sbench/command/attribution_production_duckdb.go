//go:build duckdb

package command

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	duckdbdriver "github.com/duckdb/duckdb-go/v2"
	attributionanalytics "github.com/meaningforge/metis/analytics/attribution"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	appmcp "github.com/meaningforge/metis/app/mcp"
	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/execution/backend"
	duckdbbackend "github.com/meaningforge/metis/execution/backend/duckdb"
	"github.com/spf13/cobra"
)

const (
	productionAttributionSchema  = "agent-attribution-production-v0.2"
	productionAttributionPrompt  = "v0.2"
	productionAttributionProject = "agent_attribution"
)

type productionAttributionArm string
type productionAttributionMode string

const (
	productionAttributionQuery productionAttributionArm  = "query_metrics"
	productionAttributionTool  productionAttributionArm  = "attribute_metric"
	productionAttributionSmoke productionAttributionMode = "smoke"
	productionAttributionDiag  productionAttributionMode = "diagnostic"
)

var productionAttributionBudget = s2sbench.TaskBudget{ToolCalls: 12, Attempts: 1, Repetitions: 1}

type productionAttributionScenario struct {
	Name       string
	Question   string
	Metric     string
	Dimensions []string
	InsertSQL  string
}

func productionAttributionScenarios() []productionAttributionScenario {
	return []productionAttributionScenario{
		{
			Name: "additive_population_union", Metric: "metric:attribution.revenue",
			Question:   "Why did revenue change between July and August 2026? Show independently reconciled evidence by region and by channel.",
			Dimensions: []string{"dimension:attribution.attribution_events.region", "dimension:attribution.attribution_events.channel"},
			InsertSQL:  "INSERT INTO analytics.attribution_events VALUES ('2026-07-10','north','web','A',100,0,0),('2026-07-10','south','store','B',50,0,0),('2026-08-10','north','web','A',120,0,0),('2026-08-10','east','store','C',80,0,0)",
		},
		{
			Name: "additive_offsetting_zero_total", Metric: "metric:attribution.revenue",
			Question:   "Which customer segments offset one another when revenue was unchanged between July and August 2026?",
			Dimensions: []string{"dimension:attribution.attribution_events.segment"},
			InsertSQL:  "INSERT INTO analytics.attribution_events VALUES ('2026-07-10','all','all','A',100,0,0),('2026-07-10','all','all','B',50,0,0),('2026-08-10','all','all','A',120,0,0),('2026-08-10','all','all','B',30,0,0)",
		},
		{
			Name: "ratio_mix_rate_entry_exit", Metric: "metric:attribution.conversion_rate",
			Question:   "Why did conversion rate change between July and August 2026? Separate segment rate, mix, entry, and exit evidence.",
			Dimensions: []string{"dimension:attribution.attribution_events.segment"},
			InsertSQL:  "INSERT INTO analytics.attribution_events VALUES ('2026-07-10','all','all','A',0,20,100),('2026-07-10','all','all','B',0,5,50),('2026-08-10','all','all','A',0,36,120),('2026-08-10','all','all','C',0,16,80)",
		},
		{
			Name: "ratio_undefined_denominator", Metric: "metric:attribution.conversion_rate",
			Question:   "Explain the conversion-rate change between July and August 2026, including any segments or totals for which the rate is undefined.",
			Dimensions: []string{"dimension:attribution.attribution_events.segment"},
			InsertSQL:  "INSERT INTO analytics.attribution_events VALUES ('2026-07-10','all','all','A',0,20,100),('2026-08-10','all','all','D',0,1,0)",
		},
	}
}

type productionAttributionEvidence struct {
	Metric        string                                            `json:"metric"`
	TimeDimension string                                            `json:"time_dimension"`
	Baseline      attributionanalytics.AttributionPeriod            `json:"baseline"`
	Current       attributionanalytics.AttributionPeriod            `json:"current"`
	Strategy      attributionanalytics.AttributionStrategy          `json:"strategy"`
	Dimensions    []attributionanalytics.DimensionAttributionResult `json:"dimensions"`
}

type productionAttributionManifest struct {
	SchemaVersion string                    `json:"schema_version"`
	PromptVersion string                    `json:"prompt_version"`
	RunMode       productionAttributionMode `json:"run_mode"`
	Agent         s2sbench.AgentIdentity    `json:"agent"`
	ModelProvider string                    `json:"model_provider"`
	ModelID       string                    `json:"model_id"`
	ModelVersion  string                    `json:"model_version"`
	Scenarios     []string                  `json:"scenarios"`
	Budget        s2sbench.TaskBudget       `json:"budget"`
	StartedAt     time.Time                 `json:"started_at"`
}

type productionAttributionAttempt struct {
	Arm               productionAttributionArm    `json:"arm"`
	Scenario          string                      `json:"scenario"`
	DurationMS        int64                       `json:"duration_ms"`
	ToolCalls         int                         `json:"tool_calls"`
	ContextTokens     int                         `json:"context_tokens"`
	OutputTokens      int                         `json:"output_tokens"`
	ToolTrace         []s2sbench.ToolCallEvidence `json:"tool_trace,omitempty"`
	SemanticVerdict   s2sbench.Verdict            `json:"semantic_verdict"`
	SemanticReason    string                      `json:"semantic_reason,omitempty"`
	ContractVerdict   s2sbench.Verdict            `json:"contract_verdict"`
	ContractReason    string                      `json:"contract_reason,omitempty"`
	WorkflowVerdict   s2sbench.Verdict            `json:"workflow_verdict"`
	WorkflowReason    string                      `json:"workflow_reason,omitempty"`
	AnswerFingerprint string                      `json:"answer_fingerprint,omitempty"`
	RawOutput         string                      `json:"raw_output,omitempty"`
}

type productionAttributionSummary struct {
	Arm                     productionAttributionArm `json:"arm"`
	Total                   int                      `json:"total"`
	SemanticCorrect         int                      `json:"semantic_correct"`
	SemanticWrong           int                      `json:"semantic_wrong"`
	SemanticFailed          int                      `json:"semantic_failed"`
	ContractCorrect         int                      `json:"contract_correct"`
	WorkflowCorrect         int                      `json:"workflow_correct"`
	DurationMS              int64                    `json:"duration_ms"`
	ToolCalls               int                      `json:"tool_calls"`
	ContextTokens           int                      `json:"context_tokens"`
	OutputTokens            int                      `json:"output_tokens"`
	SemanticAccuracyPercent float64                  `json:"semantic_accuracy_percent"`
}

type productionAttributionReport struct {
	Manifest        productionAttributionManifest  `json:"manifest"`
	Attempts        []productionAttributionAttempt `json:"attempts"`
	QueryMetrics    productionAttributionSummary   `json:"query_metrics"`
	AttributeMetric productionAttributionSummary   `json:"attribute_metric"`
	Note            string                         `json:"note"`
}

type productionAttributionEndpoint struct {
	server      *http.Server
	listener    net.Listener
	mcp         s2sbench.AgentMCP
	environment map[string]string
}

type productionAttributionRuntime struct {
	runtime   *bootstrap.Runtime
	endpoints map[productionAttributionArm]*productionAttributionEndpoint
}

const productionAttributionModel = `version: "0.2.0.dev0"
semantic_model:
  - name: attribution
    description: Governed revenue and conversion evidence for the production attribution Agent experiment.
    datasets:
      - name: attribution_events
        source: analytics.attribution_events
        fields:
          - {name: event_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.event_time}]}, dimension: {is_time: true}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.region}]}, dimension: {}}
          - {name: channel, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.channel}]}, dimension: {}}
          - {name: segment, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.segment}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.revenue}]}}
          - {name: converted, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.converted}]}}
          - {name: sessions, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: attribution_events.sessions}]}}
    metrics:
      - name: revenue
        description: Additive revenue used for period-over-period attribution.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(attribution_events.revenue)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: converted
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(attribution_events.converted)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: sessions
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(attribution_events.sessions)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: conversion_rate
        description: Converted events divided by sessions; undefined when sessions is zero.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "converted / sessions"}]}
`

type attributionCLIOptions struct {
	agentName, model, provider, modelVersion, mode, scenarioName, outputDir *string
	codexBin, claudeBin, genericBin, genericName, genericVersion            *string
	executeLive                                                             *bool
	genericArgs                                                             stringListFlag
}

func newAttributionCommand(stdout io.Writer) *cobra.Command {
	options := attributionCLIOptions{}
	command := &cobra.Command{Use: "attribution-run", Short: "Run the attribution experiment", Args: cobra.NoArgs}
	flags := command.Flags()
	options.agentName = flags.String("agent", "", "installed agent")
	options.model = flags.String("model", "", "exact model ID")
	options.provider = flags.String("provider", "", "model provider")
	options.modelVersion = flags.String("model-version", "", "exact model revision")
	options.mode = flags.String("mode", string(productionAttributionSmoke), "smoke or diagnostic")
	options.scenarioName = flags.String("scenario", "", "one frozen scenario in smoke mode")
	options.outputDir = flags.String("output", "", "new result directory")
	options.executeLive = flags.Bool("execute-live", false, "acknowledge real Agent/model invocation")
	options.codexBin = flags.String("codex-bin", "codex", "Codex CLI binary")
	options.claudeBin = flags.String("claude-bin", "claude", "Claude Code CLI binary")
	options.genericBin = flags.String("generic-bin", "", "generic s2sbench-driver-v2 wrapper")
	options.genericName = flags.String("generic-name", "", "generic Agent name")
	options.genericVersion = flags.String("generic-version", "", "generic Agent version")
	flags.Var(&options.genericArgs, "generic-arg", "generic wrapper argument; repeatable")
	for _, name := range []string{"agent", "model", "provider", "model-version", "output"} {
		_ = command.MarkFlagRequired(name)
	}
	command.RunE = func(*cobra.Command, []string) error { return runProductionAttribution(options, stdout) }
	return command
}

func runProductionAttribution(options attributionCLIOptions, stdout io.Writer) error {
	agentName, model, provider, modelVersion := options.agentName, options.model, options.provider, options.modelVersion
	mode, scenarioName, outputDir, executeLive := options.mode, options.scenarioName, options.outputDir, options.executeLive
	codexBin, claudeBin, genericBin := options.codexBin, options.claudeBin, options.genericBin
	genericName, genericVersion, genericArgs := options.genericName, options.genericVersion, options.genericArgs
	if !*executeLive {
		return fmt.Errorf("production attribution experiment requires --execute-live")
	}
	if strings.TrimSpace(*agentName) == "" || strings.TrimSpace(*model) == "" || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*modelVersion) == "" || strings.TrimSpace(*outputDir) == "" {
		return fmt.Errorf("attribution-run requires agent, model, provider, model-version, and output")
	}
	scenarios, err := selectProductionAttributionScenarios(productionAttributionMode(strings.TrimSpace(*mode)), strings.TrimSpace(*scenarioName))
	if err != nil {
		return err
	}
	if err := validateAgentProvider(*agentName, *provider); err != nil {
		return err
	}
	output, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("output directory %q already exists", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	driver, err := buildAgentDriver(*agentName, *model, *codexBin, *claudeBin, *genericBin, *genericName, *genericVersion, genericArgs)
	if err != nil {
		return err
	}
	identity, err := driver.Identity(ctx)
	if err != nil {
		return err
	}
	manifest := productionAttributionManifest{
		SchemaVersion: productionAttributionSchema, PromptVersion: productionAttributionPrompt,
		RunMode: productionAttributionMode(strings.TrimSpace(*mode)), Agent: identity,
		ModelProvider: strings.TrimSpace(*provider), ModelID: strings.TrimSpace(*model), ModelVersion: strings.TrimSpace(*modelVersion),
		Budget: productionAttributionBudget, StartedAt: time.Now().UTC(),
	}
	for _, scenario := range scenarios {
		manifest.Scenarios = append(manifest.Scenarios, scenario.Name)
	}
	partial := output + ".partial"
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(partial, 0o700); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(partial, "manifest.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Production attribution %s started; evidence: %s\n", manifest.RunMode, partial)
	root, err := os.MkdirTemp("", "metis-agent-attribution-production-")
	if err != nil {
		return err
	}
	defer removeBenchmarkRuntimeRoot(root)
	attempts := make([]productionAttributionAttempt, 0, len(scenarios)*2)
	for _, scenario := range scenarios {
		scenarioAttempts, runErr := func() ([]productionAttributionAttempt, error) {
			runtime, runtimeErr := newProductionAttributionRuntime(filepath.Join(root, scenario.Name), scenario)
			if runtimeErr != nil {
				return nil, runtimeErr
			}
			defer runtime.Close(context.Background())
			oracle, oracleErr := runtime.Oracle(ctx, scenario)
			if oracleErr != nil {
				return nil, oracleErr
			}
			result := make([]productionAttributionAttempt, 0, 2)
			for _, arm := range []productionAttributionArm{productionAttributionQuery, productionAttributionTool} {
				attempt, armErr := runProductionAttributionArm(ctx, driver, runtime, root, arm, scenario, oracle)
				result = append(result, attempt)
				name := scenario.Name + "--" + string(arm) + ".json"
				if manifest.RunMode == productionAttributionSmoke {
					name = string(arm) + ".json"
				}
				if err := writeJSONFile(filepath.Join(partial, name), attempt); err != nil {
					return result, err
				}
				fmt.Fprintf(stdout, "Scenario=%s Arm=%s Semantic=%s Contract=%s Workflow=%s Tools=%d Reason=%q\n", scenario.Name, arm, attempt.SemanticVerdict, attempt.ContractVerdict, attempt.WorkflowVerdict, attempt.ToolCalls, attempt.SemanticReason)
				if armErr != nil {
					return result, fmt.Errorf("production attribution %s/%s: %w; partial evidence retained at %s", scenario.Name, arm, armErr, partial)
				}
			}
			return result, nil
		}()
		attempts = append(attempts, scenarioAttempts...)
		if runErr != nil {
			return runErr
		}
	}
	report := productionAttributionReport{
		Manifest: manifest, Attempts: attempts,
		QueryMetrics:    summarizeProductionAttribution(productionAttributionQuery, attempts),
		AttributeMetric: summarizeProductionAttribution(productionAttributionTool, attempts),
		Note:            "One-pass diagnostic evidence is descriptive, not a repeated formal benchmark result.",
	}
	if err := writeJSONFile(filepath.Join(partial, "report.json"), report); err != nil {
		return err
	}
	if err := os.Rename(partial, output); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Production attribution %s written to %s\n", manifest.RunMode, output)
	return nil
}

func selectProductionAttributionScenarios(mode productionAttributionMode, name string) ([]productionAttributionScenario, error) {
	scenarios := productionAttributionScenarios()
	switch mode {
	case productionAttributionSmoke:
		if name == "" {
			return nil, fmt.Errorf("production attribution smoke requires --scenario")
		}
		for _, scenario := range scenarios {
			if scenario.Name == name {
				return []productionAttributionScenario{scenario}, nil
			}
		}
		return nil, fmt.Errorf("unknown production attribution scenario %q", name)
	case productionAttributionDiag:
		if name != "" {
			return nil, fmt.Errorf("production attribution diagnostic runs all scenarios; omit --scenario")
		}
		return scenarios, nil
	default:
		return nil, fmt.Errorf("production attribution mode must be smoke or diagnostic")
	}
}

func runProductionAttributionArm(ctx context.Context, driver s2sbench.AgentDriver, runtime *productionAttributionRuntime, root string, arm productionAttributionArm, scenario productionAttributionScenario, oracle productionAttributionEvidence) (attempt productionAttributionAttempt, terminalErr error) {
	attempt = productionAttributionAttempt{Arm: arm, Scenario: scenario.Name, SemanticVerdict: s2sbench.VerdictFailed, ContractVerdict: s2sbench.VerdictFailed, WorkflowVerdict: s2sbench.VerdictFailed}
	started := time.Now()
	defer func() { attempt.DurationMS = time.Since(started).Milliseconds() }()
	endpoint := runtime.endpoints[arm]
	if endpoint == nil {
		return attempt, fmt.Errorf("production attribution endpoint is unavailable")
	}
	workspace, err := os.MkdirTemp(root, "attribution-agent-")
	if err != nil {
		return attempt, err
	}
	prompt := productionAttributionAgentPrompt(arm, scenario)
	request := s2sbench.AgentRequest{Workspace: workspace, Prompt: prompt, MCP: &endpoint.mcp, Environment: endpoint.environment, Budget: productionAttributionBudget}
	session, err := driver.Open(ctx, request)
	if err != nil {
		return attempt, err
	}
	result, runErr := session.Run(ctx, request)
	closeErr := session.Close()
	attempt.ToolCalls, attempt.ContextTokens, attempt.OutputTokens = result.ToolCalls, result.ContextTokens, result.OutputTokens
	attempt.ToolTrace = append([]s2sbench.ToolCallEvidence(nil), result.ToolTrace...)
	attempt.RawOutput = strings.TrimSpace(result.Output)
	if runErr != nil {
		attempt.SemanticReason = runErr.Error()
		if s2sbench.IsAgentUnavailableError(runErr) {
			return attempt, runErr
		}
		return attempt, nil
	}
	if closeErr != nil {
		return attempt, closeErr
	}
	_, strictErr := decodeProductionAttributionContract(result.Output)
	switch {
	case strictErr != nil:
		attempt.ContractVerdict, attempt.ContractReason = s2sbench.VerdictWrong, strictErr.Error()
	default:
		attempt.ContractVerdict = s2sbench.VerdictCorrect
	}
	if err := validateProductionAttributionTrace(arm, scenario, attempt.ToolTrace); err != nil {
		attempt.WorkflowVerdict, attempt.WorkflowReason = s2sbench.VerdictWrong, err.Error()
	} else {
		attempt.WorkflowVerdict = s2sbench.VerdictCorrect
	}
	actualCanonical, err := canonicalProductionAttribution(result.Output)
	if err != nil {
		attempt.SemanticVerdict, attempt.SemanticReason = s2sbench.VerdictWrong, err.Error()
		return attempt, nil
	}
	oracleJSON, err := json.Marshal(oracle)
	if err != nil {
		return attempt, err
	}
	wantCanonical, err := canonicalProductionAttribution(string(oracleJSON))
	if err != nil {
		return attempt, err
	}
	digest := sha256.Sum256(actualCanonical)
	attempt.AnswerFingerprint = "sha256:" + hex.EncodeToString(digest[:])
	if !productionAttributionSemanticallyEqual(actualCanonical, wantCanonical) {
		attempt.SemanticVerdict, attempt.SemanticReason = s2sbench.VerdictWrong, "attribution evidence is not semantically equivalent to the production oracle"
		return attempt, nil
	}
	attempt.SemanticVerdict = s2sbench.VerdictCorrect
	return attempt, nil
}

func productionAttributionAgentPrompt(arm productionAttributionArm, scenario productionAttributionScenario) string {
	workflow := fmt.Sprintf(`Call query_metrics exactly %d times: one baseline and one current query for every requested dimension. Use model:attribution. For each period use two filters on dimension:attribution.attribution_events.event_time: gte the period start and lt the period end. For additive attribution query metric:attribution.revenue; for ratio attribution query metric:attribution.converted and metric:attribution.sessions together. Align each dimension independently and calculate the complete attribution result yourself. Do not call attribute_metric.`, 2*len(scenario.Dimensions))
	if arm == productionAttributionTool {
		workflow = "Call attribute_metric exactly once using the supplied canonical metric, time dimension, periods, and every requested dimension. Do not call query_metrics."
	}
	return fmt.Sprintf(`You are solving one controlled production metric-attribution question using only the Metis MCP tools in this session.
The project is bound by transport; omit project_id. The canonical refs below are authoritative, so do not spend tool calls rediscovering them. Baseline is [2026-07-01T00:00:00Z, 2026-08-01T00:00:00Z) and current is [2026-08-01T00:00:00Z, 2026-09-01T00:00:00Z). Do not inspect the host or database and do not invent missing rows.

%s

Both arms use this same frozen answer contract. Return exactly one JSON object without prose or a code fence:
- root: metric, time_dimension, baseline {start,end}, current {start,end}, strategy, dimensions;
- strategy is exactly additive_contribution or ratio_mix_rate;
- every dimension: dimension, member_type, and exactly one of additive or ratio;
- additive: summary {baseline_total,current_total,total_delta,contribution_defined,reconciled} and segments [{value,baseline_value,current_value,delta,contribution_pct}];
- ratio: summary {baseline_numerator,baseline_denominator,current_numerator,current_denominator,baseline_ratio,current_ratio,ratio_delta,decomposed_delta,reconciliation_residual,attribution_defined,reconciled} and segments [{value,baseline_present,current_present,baseline_numerator,baseline_denominator,current_numerator,current_denominator,baseline_rate,current_rate,baseline_weight,current_weight,baseline_defined,current_defined,segment_defined,rate_effect,mix_effect,entry_effect,exit_effect,total_effect}].
All decimal values are JSON strings without unnecessary trailing zeros. Undefined decimals are null. Member values retain their JSON scalar type. Preserve every segment, entry/exit state, zero-total state, undefined state, and reconciliation state.

Scenario: %s
Question: %s
Canonical metric: %s
Canonical time dimension: dimension:attribution.attribution_events.event_time
Requested dimensions: %v
Maximum tool calls: %d.`, workflow, scenario.Name, scenario.Question, scenario.Metric, scenario.Dimensions, productionAttributionBudget.ToolCalls)
}

func decodeProductionAttributionContract(output string) (productionAttributionEvidence, error) {
	trimmed := strings.TrimSpace(output)
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var evidence productionAttributionEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return productionAttributionEvidence{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return productionAttributionEvidence{}, fmt.Errorf("attribution answer contains trailing content")
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return productionAttributionEvidence{}, err
	}
	if err := validateProductionAttributionContractShape(evidence, root); err != nil {
		return productionAttributionEvidence{}, err
	}
	return evidence, nil
}

func validateProductionAttributionContractShape(evidence productionAttributionEvidence, root map[string]any) error {
	if evidence.Metric == "" || evidence.TimeDimension == "" || evidence.Baseline.Start == "" || evidence.Baseline.End == "" || evidence.Current.Start == "" || evidence.Current.End == "" {
		return fmt.Errorf("attribution answer is missing required semantic context")
	}
	if err := requireAttributionFields(root, "metric", "time_dimension", "baseline", "current", "strategy", "dimensions"); err != nil {
		return err
	}
	if evidence.Strategy != attributionanalytics.AttributionStrategyAdditive && evidence.Strategy != attributionanalytics.AttributionStrategyRatio {
		return fmt.Errorf("attribution answer has invalid strategy %q", evidence.Strategy)
	}
	for _, period := range []string{"baseline", "current"} {
		object, ok := root[period].(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", period)
		}
		if err := requireAttributionFields(object, "start", "end"); err != nil {
			return fmt.Errorf("%s: %w", period, err)
		}
	}
	dimensions, ok := root["dimensions"].([]any)
	if !ok || len(dimensions) != len(evidence.Dimensions) {
		return fmt.Errorf("dimensions must be an array")
	}
	for index, value := range dimensions {
		dimension, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("dimension %d must be an object", index)
		}
		if err := requireAttributionFields(dimension, "dimension", "member_type"); err != nil {
			return fmt.Errorf("dimension %d: %w", index, err)
		}
		if evidence.Dimensions[index].Dimension == "" || !validAttributionMemberType(evidence.Dimensions[index].MemberType) {
			return fmt.Errorf("dimension %d has invalid dimension or member_type", index)
		}
		hasAdditive, hasRatio := dimension["additive"] != nil, dimension["ratio"] != nil
		if hasAdditive == hasRatio {
			return fmt.Errorf("dimension %d must contain exactly one attribution strategy", index)
		}
		if hasAdditive != (evidence.Strategy == attributionanalytics.AttributionStrategyAdditive) {
			return fmt.Errorf("dimension %d strategy does not match root strategy %q", index, evidence.Strategy)
		}
		strategy := "additive"
		summaryFields := []string{"baseline_total", "current_total", "total_delta", "contribution_defined", "reconciled"}
		segmentFields := []string{"value", "baseline_value", "current_value", "delta", "contribution_pct"}
		if hasRatio {
			strategy = "ratio"
			summaryFields = []string{"baseline_numerator", "baseline_denominator", "current_numerator", "current_denominator", "baseline_ratio", "current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined", "reconciled"}
			segmentFields = []string{"value", "baseline_present", "current_present", "baseline_numerator", "baseline_denominator", "current_numerator", "current_denominator", "baseline_rate", "current_rate", "baseline_weight", "current_weight", "baseline_defined", "current_defined", "segment_defined", "rate_effect", "mix_effect", "entry_effect", "exit_effect", "total_effect"}
		}
		block, ok := dimension[strategy].(map[string]any)
		if !ok {
			return fmt.Errorf("dimension %d %s must be an object", index, strategy)
		}
		if err := requireAttributionFields(block, "summary", "segments"); err != nil {
			return fmt.Errorf("dimension %d %s: %w", index, strategy, err)
		}
		summary, ok := block["summary"].(map[string]any)
		if !ok {
			return fmt.Errorf("dimension %d %s summary must be an object", index, strategy)
		}
		if err := requireAttributionFields(summary, summaryFields...); err != nil {
			return fmt.Errorf("dimension %d %s summary: %w", index, strategy, err)
		}
		segments, ok := block["segments"].([]any)
		if !ok {
			return fmt.Errorf("dimension %d %s segments must be an array", index, strategy)
		}
		for segmentIndex, item := range segments {
			segment, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("dimension %d %s segment %d must be an object", index, strategy, segmentIndex)
			}
			if err := requireAttributionFields(segment, segmentFields...); err != nil {
				return fmt.Errorf("dimension %d %s segment %d: %w", index, strategy, segmentIndex, err)
			}
			if member := segment["value"]; member != nil {
				switch member.(type) {
				case string, float64, bool:
				default:
					return fmt.Errorf("dimension %d %s segment %d value must be a JSON scalar", index, strategy, segmentIndex)
				}
			}
		}
	}
	return nil
}

func requireAttributionFields(object map[string]any, fields ...string) error {
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return nil
}

func validAttributionMemberType(value attributionanalytics.AttributionMemberType) bool {
	switch value {
	case attributionanalytics.AttributionMemberString, attributionanalytics.AttributionMemberInteger, attributionanalytics.AttributionMemberDecimal, attributionanalytics.AttributionMemberFloat, attributionanalytics.AttributionMemberBoolean, attributionanalytics.AttributionMemberDate, attributionanalytics.AttributionMemberTime, attributionanalytics.AttributionMemberDateTime, attributionanalytics.AttributionMemberDateTimeTZ:
		return true
	default:
		return false
	}
}

var attributionDecimalFields = map[string]struct{}{
	"baseline_total": {}, "current_total": {}, "total_delta": {}, "baseline_value": {}, "current_value": {}, "delta": {}, "contribution_pct": {},
	"baseline_numerator": {}, "baseline_denominator": {}, "current_numerator": {}, "current_denominator": {}, "baseline_ratio": {}, "current_ratio": {}, "ratio_delta": {},
	"decomposed_delta": {}, "reconciliation_residual": {}, "baseline_rate": {}, "current_rate": {}, "baseline_weight": {}, "current_weight": {}, "rate_effect": {}, "mix_effect": {}, "entry_effect": {}, "exit_effect": {}, "total_effect": {},
}

func canonicalProductionAttribution(output string) ([]byte, error) {
	root, err := extractProductionAttributionJSONObject(output)
	if err != nil {
		return nil, err
	}
	normalizeProductionAttributionPresentation(root)
	delete(root, "analysis_id")
	delete(root, "time_dimension")
	delete(root, "baseline")
	delete(root, "current")
	if err := normalizeAttributionJSON(root, ""); err != nil {
		return nil, err
	}
	if dimensions, ok := root["dimensions"].([]any); ok {
		sort.Slice(dimensions, func(i, j int) bool {
			return attributionObjectKey(dimensions[i], "dimension") < attributionObjectKey(dimensions[j], "dimension")
		})
	}
	return json.Marshal(root)
}

func normalizeProductionAttributionPresentation(root map[string]any) {
	details, ok := root["dimensions_detail"].([]any)
	if !ok || len(details) == 0 {
		return
	}
	for _, detail := range details {
		if _, ok := detail.(map[string]any); !ok {
			return
		}
	}
	root["dimensions"] = details
	delete(root, "dimensions_detail")
}

func extractProductionAttributionJSONObject(output string) (map[string]any, error) {
	trimmed := strings.TrimSpace(output)
	for offset := 0; offset < len(trimmed); {
		relative := strings.IndexByte(trimmed[offset:], '{')
		if relative < 0 {
			break
		}
		start := offset + relative
		decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
		decoder.UseNumber()
		var candidate map[string]any
		if err := decoder.Decode(&candidate); err == nil {
			_, hasMetric := candidate["metric"]
			_, hasStrategy := candidate["strategy"]
			_, hasDimensions := candidate["dimensions"]
			if hasMetric && hasStrategy && hasDimensions {
				return candidate, nil
			}
		}
		if repaired, ok := repairProductionAttributionJSON(trimmed[start:]); ok {
			decoder := json.NewDecoder(strings.NewReader(repaired))
			decoder.UseNumber()
			candidate = nil
			if err := decoder.Decode(&candidate); err == nil {
				_, hasMetric := candidate["metric"]
				_, hasStrategy := candidate["strategy"]
				_, hasDimensions := candidate["dimensions"]
				if hasMetric && hasStrategy && hasDimensions {
					return candidate, nil
				}
			}
		}
		offset = start + 1
	}
	return nil, fmt.Errorf("decode semantic attribution evidence: Agent returned no complete attribution JSON object")
}

func repairProductionAttributionJSON(input string) (string, bool) {
	if input == "" || input[0] != '{' {
		return "", false
	}
	var output strings.Builder
	stack := make([]byte, 0, 8)
	inString, escaped := false, false
	for index := 0; index < len(input); index++ {
		character := input[index]
		if inString {
			output.WriteByte(character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
			output.WriteByte(character)
		case '{', '[':
			stack = append(stack, character)
			output.WriteByte(character)
		case '}', ']':
			want := byte('{')
			if character == ']' {
				want = '['
			}
			for len(stack) > 0 && stack[len(stack)-1] != want {
				if stack[len(stack)-1] == '{' {
					output.WriteByte('}')
				} else {
					output.WriteByte(']')
				}
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 {
				return "", false
			}
			stack = stack[:len(stack)-1]
			output.WriteByte(character)
			if len(stack) == 0 {
				return output.String(), true
			}
		default:
			output.WriteByte(character)
		}
	}
	if inString {
		return "", false
	}
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index] == '{' {
			output.WriteByte('}')
		} else {
			output.WriteByte(']')
		}
	}
	return output.String(), len(stack) > 0
}

func normalizeAttributionJSON(value any, field string) error {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "member_type")
		for key, child := range typed {
			if _, decimal := attributionDecimalFields[key]; decimal && child != nil {
				canonical, err := canonicalAttributionDecimal(child)
				if err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
				typed[key] = canonical
				continue
			}
			if err := normalizeAttributionJSON(child, key); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := normalizeAttributionJSON(child, field); err != nil {
				return err
			}
		}
		if field == "segments" {
			for _, child := range typed {
				if _, ok := child.(map[string]any); !ok {
					return fmt.Errorf("attribution segment has type %T", child)
				}
			}
			sort.Slice(typed, func(i, j int) bool {
				left, _ := json.Marshal(typed[i].(map[string]any)["value"])
				right, _ := json.Marshal(typed[j].(map[string]any)["value"])
				return string(left) < string(right)
			})
		}
	}
	return nil
}

func canonicalAttributionDecimal(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	default:
		return "", fmt.Errorf("decimal has type %T", value)
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return "", fmt.Errorf("invalid decimal %q", text)
	}
	return number.RatString(), nil
}

func productionAttributionSemanticallyEqual(actual, expected []byte) bool {
	var left, right any
	if json.Unmarshal(actual, &left) != nil || json.Unmarshal(expected, &right) != nil {
		return false
	}
	return equalAttributionJSON(left, right, "")
}

func equalAttributionJSON(left, right any, field string) bool {
	if _, decimal := attributionDecimalFields[field]; decimal && left != nil && right != nil {
		leftText, leftOK := left.(string)
		rightText, rightOK := right.(string)
		if !leftOK || !rightOK {
			return false
		}
		leftNumber, leftOK := new(big.Rat).SetString(leftText)
		rightNumber, rightOK := new(big.Rat).SetString(rightText)
		if !leftOK || !rightOK {
			return false
		}
		difference := new(big.Rat).Sub(leftNumber, rightNumber)
		difference.Abs(difference)
		return difference.Cmp(big.NewRat(1, 1_000_000_000_000)) <= 0
	}
	switch typed := left.(type) {
	case map[string]any:
		other, ok := right.(map[string]any)
		if !ok || len(typed) != len(other) {
			return false
		}
		for key, value := range typed {
			if !equalAttributionJSON(value, other[key], key) {
				return false
			}
		}
		return true
	case []any:
		other, ok := right.([]any)
		if !ok || len(typed) != len(other) {
			return false
		}
		for index := range typed {
			if !equalAttributionJSON(typed[index], other[index], field) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left, right)
	}
}

func attributionObjectKey(value any, key string) string {
	object, _ := value.(map[string]any)
	text, _ := object[key].(string)
	return text
}

func validateProductionAttributionTrace(arm productionAttributionArm, scenario productionAttributionScenario, trace []s2sbench.ToolCallEvidence) error {
	counts, successes := map[string]int{}, map[string]int{}
	for _, call := range trace {
		counts[call.Name]++
		if call.Status == "success" {
			successes[call.Name]++
		}
	}
	wantQueries := 2 * len(scenario.Dimensions)
	switch arm {
	case productionAttributionQuery:
		if successes[appmcp.ToolQueryMetrics] != wantQueries || successes[appmcp.ToolAttributeMetric] != 0 {
			return fmt.Errorf("query arm completed query_metrics=%d/%d attempts and attribute_metric=%d/%d; want %d successful query_metrics and no successful attribute_metric", successes[appmcp.ToolQueryMetrics], counts[appmcp.ToolQueryMetrics], successes[appmcp.ToolAttributeMetric], counts[appmcp.ToolAttributeMetric], wantQueries)
		}
	case productionAttributionTool:
		if successes[appmcp.ToolAttributeMetric] != 1 || successes[appmcp.ToolQueryMetrics] != 0 {
			return fmt.Errorf("attribute arm completed attribute_metric=%d/%d attempts and query_metrics=%d/%d; want one successful attribute_metric and no successful query_metrics", successes[appmcp.ToolAttributeMetric], counts[appmcp.ToolAttributeMetric], successes[appmcp.ToolQueryMetrics], counts[appmcp.ToolQueryMetrics])
		}
	}
	return nil
}

func summarizeProductionAttribution(arm productionAttributionArm, attempts []productionAttributionAttempt) productionAttributionSummary {
	summary := productionAttributionSummary{Arm: arm}
	for _, attempt := range attempts {
		if attempt.Arm != arm {
			continue
		}
		summary.Total++
		switch attempt.SemanticVerdict {
		case s2sbench.VerdictCorrect:
			summary.SemanticCorrect++
		case s2sbench.VerdictWrong:
			summary.SemanticWrong++
		case s2sbench.VerdictFailed:
			summary.SemanticFailed++
		}
		if attempt.ContractVerdict == s2sbench.VerdictCorrect {
			summary.ContractCorrect++
		}
		if attempt.WorkflowVerdict == s2sbench.VerdictCorrect {
			summary.WorkflowCorrect++
		}
		summary.DurationMS += attempt.DurationMS
		summary.ToolCalls += attempt.ToolCalls
		summary.ContextTokens += attempt.ContextTokens
		summary.OutputTokens += attempt.OutputTokens
	}
	if summary.Total > 0 {
		summary.SemanticAccuracyPercent = float64(summary.SemanticCorrect) * 100 / float64(summary.Total)
	}
	return summary
}

func newProductionAttributionRuntime(root string, scenario productionAttributionScenario) (*productionAttributionRuntime, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	databasePath := filepath.Join(root, "attribution.duckdb")
	if err := seedProductionAttributionDatabase(databasePath, scenario); err != nil {
		return nil, err
	}
	configPath, err := writeProductionAttributionProject(root, databasePath)
	if err != nil {
		return nil, err
	}
	backends, err := backend.NewBackendRegistry(duckdbbackend.New())
	if err != nil {
		return nil, err
	}
	projectRuntime, err := bootstrap.LoadRuntime(configPath, bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		return nil, err
	}
	runtime := &productionAttributionRuntime{runtime: projectRuntime, endpoints: map[productionAttributionArm]*productionAttributionEndpoint{}}
	handlers := map[productionAttributionArm]http.Handler{
		productionAttributionQuery: appmcp.NewObservedHTTPHandlerWithQueryMetrics(projectRuntime.Discovery, projectRuntime.Compile, projectRuntime.QueryMetrics, nil, nil),
		productionAttributionTool:  appmcp.NewObservedHTTPHandlerWithAnalytics(projectRuntime.Discovery, projectRuntime.Compile, projectRuntime.QueryMetrics, projectRuntime.AttributeMetric, nil, nil, nil),
	}
	for _, arm := range []productionAttributionArm{productionAttributionQuery, productionAttributionTool} {
		endpoint, err := startProductionAttributionEndpoint(handlers[arm])
		if err != nil {
			runtime.Close(context.Background())
			return nil, err
		}
		runtime.endpoints[arm] = endpoint
	}
	return runtime, nil
}

func (runtime *productionAttributionRuntime) Oracle(ctx context.Context, scenario productionAttributionScenario) (productionAttributionEvidence, error) {
	principalCtx := auth.WithPrincipal(ctx, &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := runtime.runtime.AttributeMetric.AttributeMetric(principalCtx, service.MetricAttributionQuery{
		ProjectID: productionAttributionProject, Metric: scenario.Metric,
		TimeDimension: "dimension:attribution.attribution_events.event_time",
		Baseline:      attributionanalytics.AttributionPeriod{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
		Current:       attributionanalytics.AttributionPeriod{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
		Dimensions:    scenario.Dimensions,
	})
	if err != nil {
		return productionAttributionEvidence{}, err
	}
	return productionAttributionEvidence{Metric: result.Metric, TimeDimension: result.TimeDimension, Baseline: result.Baseline, Current: result.Current, Strategy: result.Strategy, Dimensions: result.Dimensions}, nil
}

func seedProductionAttributionDatabase(path string, scenario productionAttributionScenario) error {
	connector, err := duckdbdriver.NewConnector(path, nil)
	if err != nil {
		return err
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	statement := `DROP SCHEMA IF EXISTS analytics CASCADE;
CREATE SCHEMA analytics;
CREATE TABLE analytics.attribution_events(event_time TIMESTAMP, region VARCHAR, channel VARCHAR, segment VARCHAR, revenue DECIMAL(20,4), converted DECIMAL(20,4), sessions DECIMAL(20,4));
` + scenario.InsertSQL
	if _, err := database.Exec(statement); err != nil {
		return fmt.Errorf("seed attribution scenario %q: %w", scenario.Name, err)
	}
	return nil
}

func writeProductionAttributionProject(root, databasePath string) (string, error) {
	if err := os.WriteFile(filepath.Join(root, "attribution.ossie.yaml"), []byte(productionAttributionModel), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(root, "project.yaml"), []byte("semantic_sources:\n  attribution:\n    path: ./attribution.ossie.yaml\n"), 0o600); err != nil {
		return "", err
	}
	datasources := fmt.Sprintf("duckdb-local:\n  type: duckdb\n  config:\n    path: %q\n  policy:\n    query_timeout: 30s\n    max_rows: 1000\n    max_bytes: 8388608\n    max_concurrency: 1\n", databasePath)
	if err := os.WriteFile(filepath.Join(root, "datasources.yaml"), []byte(datasources), 0o600); err != nil {
		return "", err
	}
	deployment := "version: 1\nprojects:\n  " + productionAttributionProject + ":\n    path: ./project.yaml\n    data_source: duckdb-local\ndata_sources:\n  path: ./datasources.yaml\n"
	path := filepath.Join(root, "metis.yaml")
	if err := os.WriteFile(path, []byte(deployment), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func startProductionAttributionEndpoint(handler http.Handler) (*productionAttributionEndpoint, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token, err := randomBearerToken()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	principalHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal := &auth.Principal{TenantID: "s2sbench", SubjectID: "attribution-production", APIKeyID: "ephemeral", Scopes: []string{auth.ScopeSemanticRead, auth.ScopeSemanticCompile, auth.ScopeSemanticExecute}}
		handler.ServeHTTP(writer, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
	})
	server := &http.Server{Handler: bearerAuth(token, principalHandler)}
	go func() { _ = server.Serve(listener) }()
	return &productionAttributionEndpoint{
		server: server, listener: listener,
		mcp:         s2sbench.AgentMCP{Name: benchmarkMCPServerName, URL: "http://" + listener.Addr().String(), BearerTokenEnvVar: s2sbenchMCPTokenEnv, HTTPHeaders: map[string]string{appmcp.ProjectHeaderKey: productionAttributionProject}},
		environment: map[string]string{s2sbenchMCPTokenEnv: token},
	}, nil
}

func (runtime *productionAttributionRuntime) Close(ctx context.Context) {
	if runtime == nil {
		return
	}
	for _, endpoint := range runtime.endpoints {
		if endpoint.server != nil {
			_ = endpoint.server.Shutdown(ctx)
		}
		if endpoint.listener != nil {
			_ = endpoint.listener.Close()
		}
	}
	if runtime.runtime != nil && runtime.runtime.Execution != nil {
		_ = runtime.runtime.Execution.Close(ctx)
	}
}
