//go:build duckdb

package command

import (
	"bytes"
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
	comparison "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	appmcp "github.com/meaningforge/metis/app/mcp"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/execution/backend"
	duckdbbackend "github.com/meaningforge/metis/execution/backend/duckdb"
	"github.com/meaningforge/metis/query"
	"github.com/spf13/cobra"
)

const (
	comparisonSmokeSchema = "agent-comparison-smoke-v0.4"
	comparisonPrompt      = "v0.2"
	comparisonProject     = "agent_comparison"
)

type comparisonSmokeArm string
type comparisonRunMode string

const (
	comparisonArmQuery   comparisonSmokeArm = "query_metrics"
	comparisonArmCompare comparisonSmokeArm = "compare_metrics"
	comparisonModeSmoke  comparisonRunMode  = "smoke"
	comparisonModeDiag   comparisonRunMode  = "diagnostic"
)

var comparisonSmokeBudget = s2sbench.TaskBudget{ToolCalls: 10, Attempts: 1, Repetitions: 1}

type comparisonSmokeEvidence struct {
	TimeDimension string            `json:"time_dimension"`
	Baseline      comparison.Period `json:"baseline"`
	Current       comparison.Period `json:"current"`
	Metrics       []string          `json:"metrics"`
	Dimensions    []string          `json:"dimensions"`
	Rows          []comparison.Row  `json:"rows"`
}

type comparisonSmokeManifest struct {
	SchemaVersion string                 `json:"schema_version"`
	PromptVersion string                 `json:"prompt_version"`
	RunMode       comparisonRunMode      `json:"run_mode"`
	Agent         s2sbench.AgentIdentity `json:"agent"`
	ModelProvider string                 `json:"model_provider"`
	ModelID       string                 `json:"model_id"`
	ModelVersion  string                 `json:"model_version"`
	Scenarios     []string               `json:"scenarios"`
	Budget        s2sbench.TaskBudget    `json:"budget"`
	StartedAt     time.Time              `json:"started_at"`
}

type comparisonSmokeAttempt struct {
	Arm               comparisonSmokeArm          `json:"arm"`
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
	Evidence          *comparisonSmokeEvidence    `json:"semantic_evidence,omitempty"`
	RawOutput         string                      `json:"raw_output,omitempty"`
}

type comparisonSmokeFlexibleEvidence struct {
	TimeDimension json.RawMessage              `json:"time_dimension"`
	Baseline      json.RawMessage              `json:"baseline"`
	Current       json.RawMessage              `json:"current"`
	Metrics       []string                     `json:"metrics"`
	Dimensions    []string                     `json:"dimensions"`
	Rows          []comparisonSmokeFlexibleRow `json:"rows"`
}

type comparisonSmokeFlexibleRow struct {
	Members         json.RawMessage `json:"members"`
	BaselinePresent bool            `json:"baseline_present"`
	CurrentPresent  bool            `json:"current_present"`
	Values          json.RawMessage `json:"values"`
}

type comparisonSmokeReport struct {
	Manifest comparisonSmokeManifest  `json:"manifest"`
	Attempts []comparisonSmokeAttempt `json:"attempts"`
	Query    comparisonArmSummary     `json:"query_metrics"`
	Compare  comparisonArmSummary     `json:"compare_metrics"`
	Note     string                   `json:"note"`
}

type comparisonArmSummary struct {
	Arm                  comparisonSmokeArm `json:"arm"`
	Total                int                `json:"total"`
	SemanticCorrect      int                `json:"semantic_correct"`
	SemanticWrong        int                `json:"semantic_wrong"`
	SemanticFailed       int                `json:"semantic_failed"`
	ContractCorrect      int                `json:"contract_correct"`
	WorkflowCorrect      int                `json:"workflow_correct"`
	DurationMS           int64              `json:"duration_ms"`
	ToolCalls            int                `json:"tool_calls"`
	ContextTokens        int                `json:"context_tokens"`
	OutputTokens         int                `json:"output_tokens"`
	SemanticAccuracyRate float64            `json:"semantic_accuracy_percent"`
}

type comparisonScenario struct {
	Name       string
	Question   string
	Metrics    []string
	Dimensions []string
	Filters    []query.Filter
}

type comparisonSmokeEndpoint struct {
	server      *http.Server
	listener    net.Listener
	mcp         s2sbench.AgentMCP
	environment map[string]string
}

type comparisonSmokeRuntime struct {
	runtime   *bootstrap.Runtime
	endpoints map[comparisonSmokeArm]*comparisonSmokeEndpoint
}

const comparisonQuestion = "Compare revenue and sessions between July and August 2026 by region. Include every region present in either period, values, delta, and percent change; preserve missing rows, null metric values, and zero baselines."

func frozenComparisonScenarios() []comparisonScenario {
	metricRevenue := "metric:comparison.revenue"
	metricSessions := "metric:comparison.sessions"
	region := "dimension:comparison.events.region"
	return []comparisonScenario{
		{Name: "population_union_multi_metric", Question: comparisonQuestion, Metrics: []string{metricRevenue, metricSessions}, Dimensions: []string{region}},
		{Name: "ungrouped_multi_metric_totals", Question: "Compare total revenue and total sessions between July and August 2026 without grouping. Return both period values, delta, and percent change.", Metrics: []string{metricRevenue, metricSessions}},
		{Name: "zero_baseline_region", Question: "Compare revenue and sessions between July and August 2026 for the north region, grouped by region. Preserve the zero baseline and report whether percent change is defined.", Metrics: []string{metricRevenue, metricSessions}, Dimensions: []string{region}, Filters: []query.Filter{{Field: region, Operator: query.FilterEQ, Value: "north"}}},
		{Name: "null_entry_exit_revenue", Question: "Compare revenue between July and August 2026 by region, restricted to null_value, south, and west. Preserve SQL NULL, entering, and exiting members without replacing them with zero.", Metrics: []string{metricRevenue}, Dimensions: []string{region}, Filters: []query.Filter{{Field: region, Operator: query.FilterIN, Value: []any{"null_value", "south", "west"}}}},
	}
}

const comparisonModel = `version: "0.2.0.dev0"
semantic_model:
  - name: comparison
    description: Governed regional revenue and session metrics for exact period comparison.
    datasets:
      - name: events
        source: analytics.comparison_events
        fields:
          - {name: event_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: events.event_time}]}, dimension: {is_time: true}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: events.region}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: events.revenue}]}}
          - {name: sessions, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: events.sessions}]}}
    metrics:
      - name: revenue
        description: Total regional revenue.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(events.revenue)"}]}
      - name: sessions
        description: Total regional sessions.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(events.sessions)"}]}
`

type comparisonCLIOptions struct {
	agentName, model, provider, modelVersion, mode, scenarioName, outputDir *string
	codexBin, claudeBin, genericBin, genericName, genericVersion            *string
	executeLive                                                             *bool
	genericArgs                                                             stringListFlag
}

func newComparisonCommand(use, short string, stdout io.Writer) *cobra.Command {
	options := comparisonCLIOptions{}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
	flags := command.Flags()
	options.agentName = flags.String("agent", "", "installed agent: codex, claude-code, or generic")
	options.model = flags.String("model", "", "exact model ID")
	options.provider = flags.String("provider", "", "model provider")
	options.modelVersion = flags.String("model-version", "", "exact model revision")
	options.mode = flags.String("mode", string(comparisonModeSmoke), "smoke or diagnostic")
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
	command.RunE = func(*cobra.Command, []string) error { return runComparison(options, stdout) }
	return command
}

func runComparisonLive(args []string, stdout io.Writer) error {
	command := newComparisonCommand("comparison-run", "Run the comparison experiment", stdout)
	command.SetArgs(args)
	command.SilenceErrors = true
	command.SilenceUsage = true
	return command.Execute()
}

func runComparison(options comparisonCLIOptions, stdout io.Writer) error {
	agentName, model, provider, modelVersion := options.agentName, options.model, options.provider, options.modelVersion
	mode, scenarioName, outputDir, executeLive := options.mode, options.scenarioName, options.outputDir, options.executeLive
	codexBin, claudeBin, genericBin := options.codexBin, options.claudeBin, options.genericBin
	genericName, genericVersion, genericArgs := options.genericName, options.genericVersion, options.genericArgs
	if !*executeLive {
		return fmt.Errorf("comparison experiment requires --execute-live")
	}
	if strings.TrimSpace(*agentName) == "" || strings.TrimSpace(*model) == "" || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*modelVersion) == "" || strings.TrimSpace(*outputDir) == "" {
		return fmt.Errorf("comparison-run requires --agent, --model, --provider, --model-version, and --output")
	}
	runMode := comparisonRunMode(strings.TrimSpace(*mode))
	scenarios, err := selectComparisonScenarios(runMode, strings.TrimSpace(*scenarioName))
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
	driver, err := buildAgentDriver(*agentName, *model, *codexBin, *claudeBin, *genericBin, *genericName, *genericVersion, genericArgs)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	identity, err := driver.Identity(ctx)
	if err != nil {
		return err
	}
	manifest := comparisonSmokeManifest{
		SchemaVersion: comparisonSmokeSchema, PromptVersion: comparisonPrompt, RunMode: runMode,
		Agent: identity, ModelProvider: strings.TrimSpace(*provider), ModelID: strings.TrimSpace(*model), ModelVersion: strings.TrimSpace(*modelVersion),
		Budget: comparisonSmokeBudget, StartedAt: time.Now().UTC(),
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
	fmt.Fprintf(stdout, "Agent comparison %s started; incremental evidence: %s\n", runMode, partial)
	root, err := os.MkdirTemp("", "metis-agent-comparison-")
	if err != nil {
		return err
	}
	defer removeBenchmarkRuntimeRoot(root)
	runtime, err := newComparisonSmokeRuntime(root)
	if err != nil {
		return err
	}
	defer runtime.Close(context.Background())
	attempts := make([]comparisonSmokeAttempt, 0, len(scenarios)*2)
	for _, scenario := range scenarios {
		for _, arm := range []comparisonSmokeArm{comparisonArmQuery, comparisonArmCompare} {
			attempt, runErr := runComparisonSmokeArm(ctx, driver, runtime, root, arm, scenario)
			attempts = append(attempts, attempt)
			filename := scenario.Name + "--" + string(arm) + ".json"
			if runMode == comparisonModeSmoke {
				filename = string(arm) + ".json"
			}
			if err := writeJSONFile(filepath.Join(partial, filename), attempt); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Scenario=%s Arm=%s Semantic=%s Contract=%s Workflow=%s Tools=%d Context=%d Output=%d Reason=%q\n", scenario.Name, arm, attempt.SemanticVerdict, attempt.ContractVerdict, attempt.WorkflowVerdict, attempt.ToolCalls, attempt.ContextTokens, attempt.OutputTokens, attempt.SemanticReason)
			if runErr != nil {
				return fmt.Errorf("comparison %s arm %s scenario %s: %w; partial evidence retained at %s", runMode, arm, scenario.Name, runErr, partial)
			}
		}
	}
	report := comparisonSmokeReport{
		Manifest: manifest, Attempts: attempts,
		Query: summarizeComparisonArm(comparisonArmQuery, attempts), Compare: summarizeComparisonArm(comparisonArmCompare, attempts),
		Note: "A one-pass diagnostic is descriptive setup evidence; it is not a repeated formal benchmark result.",
	}
	if err := writeJSONFile(filepath.Join(partial, "report.json"), report); err != nil {
		return err
	}
	if err := os.Rename(partial, output); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Agent comparison %s written to %s\n", runMode, output)
	return nil
}

func selectComparisonScenarios(mode comparisonRunMode, name string) ([]comparisonScenario, error) {
	scenarios := frozenComparisonScenarios()
	switch mode {
	case comparisonModeSmoke:
		if name == "" {
			return []comparisonScenario{scenarios[0]}, nil
		}
		for _, scenario := range scenarios {
			if scenario.Name == name {
				return []comparisonScenario{scenario}, nil
			}
		}
		return nil, fmt.Errorf("unknown comparison scenario %q", name)
	case comparisonModeDiag:
		if name != "" {
			return nil, fmt.Errorf("comparison diagnostic runs all frozen scenarios; omit --scenario")
		}
		return scenarios, nil
	default:
		return nil, fmt.Errorf("comparison mode must be smoke or diagnostic")
	}
}

func summarizeComparisonArm(arm comparisonSmokeArm, attempts []comparisonSmokeAttempt) comparisonArmSummary {
	summary := comparisonArmSummary{Arm: arm}
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
		summary.SemanticAccuracyRate = float64(summary.SemanticCorrect) * 100 / float64(summary.Total)
	}
	return summary
}

func runComparisonSmokeArm(ctx context.Context, driver s2sbench.AgentDriver, runtime *comparisonSmokeRuntime, root string, arm comparisonSmokeArm, scenario comparisonScenario) (attempt comparisonSmokeAttempt, terminalErr error) {
	attempt = comparisonSmokeAttempt{
		Arm: arm, Scenario: scenario.Name, SemanticVerdict: s2sbench.VerdictFailed,
		ContractVerdict: s2sbench.VerdictFailed, WorkflowVerdict: s2sbench.VerdictFailed,
	}
	started := time.Now()
	defer func() { attempt.DurationMS = time.Since(started).Milliseconds() }()
	endpoint := runtime.endpoints[arm]
	if endpoint == nil {
		return attempt, fmt.Errorf("comparison endpoint is unavailable")
	}
	workspace, err := os.MkdirTemp(filepath.Join(root), "workspace-"+string(arm)+"-")
	if err != nil {
		return attempt, err
	}
	prompt := comparisonAgentPrompt(arm, scenario)
	request := s2sbench.AgentRequest{Workspace: workspace, Prompt: prompt, MCP: &endpoint.mcp, Environment: endpoint.environment, Budget: comparisonSmokeBudget}
	session, err := driver.Open(ctx, request)
	if err != nil {
		return attempt, err
	}
	result, runErr := session.Run(ctx, request)
	closeErr := session.Close()
	attempt.ToolCalls = result.ToolCalls
	attempt.ContextTokens = result.ContextTokens
	attempt.OutputTokens = result.OutputTokens
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
	strictEvidence, strictErr := decodeComparisonSmokeEvidence(result.Output)
	switch {
	case strictErr != nil:
		attempt.ContractVerdict = s2sbench.VerdictWrong
		attempt.ContractReason = strictErr.Error()
	case !reflect.DeepEqual(strictEvidence, expectedComparisonEvidence(scenario.Name)):
		attempt.ContractVerdict = s2sbench.VerdictWrong
		attempt.ContractReason = "final comparison evidence does not exactly match the typed, ordered contract"
	default:
		attempt.ContractVerdict = s2sbench.VerdictCorrect
	}
	if err := validateComparisonToolTrace(arm, attempt.ToolTrace); err != nil {
		attempt.WorkflowVerdict = s2sbench.VerdictWrong
		attempt.WorkflowReason = err.Error()
	} else {
		attempt.WorkflowVerdict = s2sbench.VerdictCorrect
	}
	evidence, decodeErr := decodeComparisonSemanticEvidence(result.Output)
	if decodeErr != nil {
		attempt.SemanticVerdict = s2sbench.VerdictWrong
		attempt.SemanticReason = decodeErr.Error()
		return attempt, nil
	}
	attempt.Evidence = &evidence
	want, err := normalizeComparisonSemanticEvidence(expectedComparisonEvidence(scenario.Name))
	if err != nil {
		return attempt, fmt.Errorf("normalize frozen semantic oracle: %w", err)
	}
	if !comparisonSemanticallyEqual(evidence, want) {
		fingerprint, fingerprintErr := fingerprintComparisonSmokeEvidence(evidence)
		if fingerprintErr != nil {
			return attempt, fingerprintErr
		}
		attempt.AnswerFingerprint = fingerprint
		attempt.SemanticVerdict = s2sbench.VerdictWrong
		attempt.SemanticReason = "final comparison evidence is not semantically equivalent to the frozen oracle"
		return attempt, nil
	}
	fingerprint, err := fingerprintComparisonSmokeEvidence(want)
	if err != nil {
		return attempt, err
	}
	attempt.AnswerFingerprint = fingerprint
	attempt.SemanticVerdict = s2sbench.VerdictCorrect
	attempt.SemanticReason = ""
	return attempt, nil
}

func comparisonAgentPrompt(arm comparisonSmokeArm, scenario comparisonScenario) string {
	workflow := "The compare_metrics tool is intentionally unavailable. Discover the governed refs, call query_metrics exactly twice (once per period with otherwise identical metrics and grain), then align tuples and calculate the final evidence yourself."
	if arm == comparisonArmCompare {
		workflow = "Discover the governed refs, then call compare_metrics exactly once. Do not reconstruct the comparison with query_metrics. Copy its complete typed evidence, omitting only analysis_id."
	}
	return fmt.Sprintf(`You are solving one controlled metric-comparison smoke question using only the production Metis MCP tools exposed in this session.
Do not inspect the host, use shell/database tools, or invent missing rows. July is baseline [2026-07-01T00:00:00Z, 2026-08-01T00:00:00Z); August is current [2026-08-01T00:00:00Z, 2026-09-01T00:00:00Z).
The project context is already bound by the transport. Omit project_id from every tool call; the model name is not a project ID.

%s

Return exactly one JSON object with keys time_dimension, baseline, current, metrics, dimensions, and rows, matching the compare_metrics result contract but without analysis_id. Use canonical refs and canonical metric/dimension order. Every row must contain members, baseline_present, current_present, and values. Every metric value must contain metric, baseline_value, current_value, delta, percent_change, change_defined, and percent_defined. Decimal values are JSON strings without unnecessary trailing zeros; undefined values are JSON null. Preserve absent rows, SQL null, and numeric zero as distinct states. Do not wrap the JSON in prose or a code fence.

Question: %s
Maximum tool calls: %d.`, workflow, scenario.Question, comparisonSmokeBudget.ToolCalls)
}

func decodeComparisonSmokeEvidence(output string) (comparisonSmokeEvidence, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(output)))
	decoder.DisallowUnknownFields()
	var evidence comparisonSmokeEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return comparisonSmokeEvidence{}, fmt.Errorf("decode comparison evidence: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return comparisonSmokeEvidence{}, fmt.Errorf("Agent returned more than one JSON value")
		}
		return comparisonSmokeEvidence{}, err
	}
	return evidence, nil
}

// decodeComparisonSemanticEvidence judges analytical meaning independently of
// the presentation contract. In particular, member mappings may be emitted as
// either the production typed array or a dimension-to-value object, and row,
// metric, and dimension ordering does not affect the semantic verdict.
func decodeComparisonSemanticEvidence(output string) (comparisonSmokeEvidence, error) {
	object, err := extractComparisonJSONObject(output)
	if err != nil {
		return comparisonSmokeEvidence{}, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(object))
	decoder.DisallowUnknownFields()
	var evidence comparisonSmokeFlexibleEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return comparisonSmokeEvidence{}, fmt.Errorf("decode semantic comparison evidence: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return comparisonSmokeEvidence{}, fmt.Errorf("Agent returned more than one JSON value")
		}
		return comparisonSmokeEvidence{}, err
	}
	return normalizeFlexibleComparisonEvidence(evidence)
}

func extractComparisonJSONObject(output string) (string, error) {
	trimmed := strings.TrimSpace(output)
	start := strings.IndexByte(trimmed, '{')
	end := strings.LastIndexByte(trimmed, '}')
	if start < 0 || end < start {
		return "", fmt.Errorf("decode semantic comparison evidence: Agent returned no JSON object")
	}
	return trimmed[start : end+1], nil
}

func normalizeComparisonSemanticEvidence(evidence comparisonSmokeEvidence) (comparisonSmokeEvidence, error) {
	flexible := comparisonSmokeFlexibleEvidence{
		Metrics:    append([]string(nil), evidence.Metrics...),
		Dimensions: append([]string(nil), evidence.Dimensions...),
		Rows:       make([]comparisonSmokeFlexibleRow, 0, len(evidence.Rows)),
	}
	flexible.TimeDimension, _ = json.Marshal(evidence.TimeDimension)
	flexible.Baseline, _ = json.Marshal(evidence.Baseline)
	flexible.Current, _ = json.Marshal(evidence.Current)
	for _, row := range evidence.Rows {
		members, err := json.Marshal(row.Members)
		if err != nil {
			return comparisonSmokeEvidence{}, err
		}
		values, err := json.Marshal(row.Values)
		if err != nil {
			return comparisonSmokeEvidence{}, err
		}
		flexible.Rows = append(flexible.Rows, comparisonSmokeFlexibleRow{
			Members: members, BaselinePresent: row.BaselinePresent, CurrentPresent: row.CurrentPresent,
			Values: values,
		})
	}
	return normalizeFlexibleComparisonEvidence(flexible)
}

func normalizeFlexibleComparisonEvidence(evidence comparisonSmokeFlexibleEvidence) (comparisonSmokeEvidence, error) {
	normalized := comparisonSmokeEvidence{
		TimeDimension: decodeFlexibleTimeDimension(evidence.TimeDimension),
		Baseline:      decodeFlexibleComparisonPeriod(evidence.Baseline),
		Current:       decodeFlexibleComparisonPeriod(evidence.Current),
		Metrics:       append([]string(nil), evidence.Metrics...),
		Dimensions:    append([]string(nil), evidence.Dimensions...),
		Rows:          make([]comparison.Row, 0, len(evidence.Rows)),
	}
	sort.Strings(normalized.Metrics)
	sort.Strings(normalized.Dimensions)
	for index, row := range evidence.Rows {
		members, err := normalizeComparisonMembers(row.Members, evidence.Dimensions)
		if err != nil {
			return comparisonSmokeEvidence{}, fmt.Errorf("normalize row %d members: %w", index, err)
		}
		values, err := normalizeComparisonValues(row.Values)
		if err != nil {
			return comparisonSmokeEvidence{}, fmt.Errorf("normalize row %d values: %w", index, err)
		}
		normalized.Rows = append(normalized.Rows, comparison.Row{
			Members: members, BaselinePresent: row.BaselinePresent, CurrentPresent: row.CurrentPresent, Values: values,
		})
	}
	sort.Slice(normalized.Rows, func(i, j int) bool {
		return comparisonSemanticRowKey(normalized.Rows[i]) < comparisonSemanticRowKey(normalized.Rows[j])
	})
	return normalized, nil
}

func decodeFlexibleTimeDimension(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var object struct {
		Dimension string `json:"dimension"`
		Name      string `json:"name"`
	}
	if json.Unmarshal(raw, &object) == nil {
		if object.Dimension != "" {
			return object.Dimension
		}
		return object.Name
	}
	return ""
}

func normalizeComparisonValues(raw json.RawMessage) ([]comparison.MetricValue, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("values is required")
	}
	var entries []json.RawMessage
	var metricKeys []string
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, err
		}
		metricKeys = make([]string, len(entries))
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		metricKeys = make([]string, 0, len(object))
		for metric := range object {
			metricKeys = append(metricKeys, metric)
		}
		sort.Strings(metricKeys)
		for _, metric := range metricKeys {
			entries = append(entries, object[metric])
		}
	default:
		return nil, fmt.Errorf("values must be an array or metric-value object")
	}
	values := make([]comparison.MetricValue, 0, len(entries))
	for index, entry := range entries {
		var flexible struct {
			Metric         string          `json:"metric"`
			BaselineValue  json.RawMessage `json:"baseline_value"`
			CurrentValue   json.RawMessage `json:"current_value"`
			Delta          json.RawMessage `json:"delta"`
			PercentChange  json.RawMessage `json:"percent_change"`
			ChangeDefined  bool            `json:"change_defined"`
			PercentDefined bool            `json:"percent_defined"`
		}
		if err := json.Unmarshal(entry, &flexible); err != nil {
			return nil, err
		}
		if flexible.Metric == "" && index < len(metricKeys) {
			flexible.Metric = metricKeys[index]
		}
		baseline, err := normalizeComparisonDecimal(flexible.BaselineValue)
		if err != nil {
			return nil, fmt.Errorf("metric %q baseline_value: %w", flexible.Metric, err)
		}
		current, err := normalizeComparisonDecimal(flexible.CurrentValue)
		if err != nil {
			return nil, fmt.Errorf("metric %q current_value: %w", flexible.Metric, err)
		}
		delta, err := normalizeComparisonDecimal(flexible.Delta)
		if err != nil {
			return nil, fmt.Errorf("metric %q delta: %w", flexible.Metric, err)
		}
		percent, err := normalizeComparisonDecimal(flexible.PercentChange)
		if err != nil {
			return nil, fmt.Errorf("metric %q percent_change: %w", flexible.Metric, err)
		}
		values = append(values, comparison.MetricValue{
			Metric: flexible.Metric, BaselineValue: baseline, CurrentValue: current, Delta: delta, PercentChange: percent,
			ChangeDefined: flexible.ChangeDefined, PercentDefined: flexible.PercentDefined,
		})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Metric < values[j].Metric })
	return values, nil
}

func normalizeComparisonDecimal(raw json.RawMessage) (*comparison.DecimalString, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var text string
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
	} else {
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return nil, fmt.Errorf("decimal value must be a JSON string, number, or null")
		}
		text = number.String()
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", text)
	}
	canonical := comparison.DecimalString(number.RatString())
	return &canonical, nil
}

func decodeFlexibleComparisonPeriod(raw json.RawMessage) comparison.Period {
	var period comparison.Period
	if json.Unmarshal(raw, &period) == nil {
		return period
	}
	var start string
	if json.Unmarshal(raw, &start) == nil {
		period.Start = start
	}
	return period
}

func normalizeComparisonMembers(raw json.RawMessage, dimensions []string) ([]comparison.Member, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("members is required")
	}
	var members []comparison.Member
	switch trimmed[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		members = make([]comparison.Member, 0, len(items))
		for index, item := range items {
			member := comparison.Member{}
			if itemTrimmed := bytes.TrimSpace(item); len(itemTrimmed) > 0 && itemTrimmed[0] == '{' {
				var object struct {
					Dimension string          `json:"dimension"`
					Name      string          `json:"name"`
					Value     json.RawMessage `json:"value"`
				}
				if err := json.Unmarshal(item, &object); err != nil {
					return nil, err
				}
				member.Dimension = object.Dimension
				if member.Dimension == "" {
					member.Dimension = object.Name
				}
				member.Value = object.Value
			} else {
				if index >= len(dimensions) {
					return nil, fmt.Errorf("positional member %d has no matching dimension", index)
				}
				member.Dimension = dimensions[index]
				member.Value = item
			}
			members = append(members, member)
		}
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		members = make([]comparison.Member, 0, len(object))
		for dimension, value := range object {
			members = append(members, comparison.Member{Dimension: dimension, Value: value})
		}
	default:
		return nil, fmt.Errorf("members must be a typed array or dimension-value object")
	}
	for index := range members {
		value, memberType, err := normalizeComparisonMemberValue(members[index].Value)
		if err != nil {
			return nil, fmt.Errorf("member %d: %w", index, err)
		}
		members[index].Value = value
		members[index].MemberType = memberType
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Dimension != members[j].Dimension {
			return members[i].Dimension < members[j].Dimension
		}
		return string(members[i].Value) < string(members[j].Value)
	})
	return members, nil
}

func comparisonSemanticallyEqual(actual, expected comparisonSmokeEvidence) bool {
	// Exact requested periods are frozen input, not an analytical conclusion the
	// Agent must repeat. Time/period echo representation belongs to contract
	// grading; metric/dimension inventories and row evidence remain semantic.
	actual.TimeDimension, expected.TimeDimension = "", ""
	actual.Baseline, actual.Current = comparison.Period{}, comparison.Period{}
	expected.Baseline, expected.Current = comparison.Period{}, comparison.Period{}
	return reflect.DeepEqual(actual, expected)
}

func normalizeComparisonMemberValue(raw json.RawMessage) (json.RawMessage, comparison.MemberType, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, "", err
	}
	var memberType comparison.MemberType
	switch typed := value.(type) {
	case nil:
	case string:
		memberType = comparison.MemberString
	case bool:
		memberType = comparison.MemberBoolean
	case json.Number:
		memberType = comparison.MemberInteger
		if strings.ContainsAny(typed.String(), ".eE") {
			memberType = comparison.MemberDecimal
		}
	default:
		return nil, "", fmt.Errorf("dimension member must be a JSON scalar")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return canonical, memberType, nil
}

func comparisonSemanticRowKey(row comparison.Row) string {
	encoded, _ := json.Marshal(row.Members)
	return string(encoded)
}

func fingerprintComparisonSmokeEvidence(evidence comparisonSmokeEvidence) (string, error) {
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateComparisonToolTrace(arm comparisonSmokeArm, trace []s2sbench.ToolCallEvidence) error {
	counts := map[string]int{}
	successes := map[string]int{}
	for _, call := range trace {
		counts[call.Name]++
		if call.Status == "success" {
			successes[call.Name]++
		}
	}
	switch arm {
	case comparisonArmQuery:
		if counts[appmcp.ToolQueryMetrics] != 2 || counts[appmcp.ToolCompareMetrics] != 0 || successes[appmcp.ToolQueryMetrics] != 2 {
			return fmt.Errorf("query_metrics arm invoked query_metrics=%d (%d successful) compare_metrics=%d, want 2 (2 successful)/0", counts[appmcp.ToolQueryMetrics], successes[appmcp.ToolQueryMetrics], counts[appmcp.ToolCompareMetrics])
		}
	case comparisonArmCompare:
		if counts[appmcp.ToolCompareMetrics] != 1 || counts[appmcp.ToolQueryMetrics] != 0 || successes[appmcp.ToolCompareMetrics] != 1 {
			return fmt.Errorf("compare_metrics arm invoked compare_metrics=%d (%d successful) query_metrics=%d, want 1 (1 successful)/0", counts[appmcp.ToolCompareMetrics], successes[appmcp.ToolCompareMetrics], counts[appmcp.ToolQueryMetrics])
		}
	default:
		return fmt.Errorf("unknown comparison arm %q", arm)
	}
	return nil
}

func expectedComparisonSmokeEvidence() comparisonSmokeEvidence {
	return expectedComparisonEvidence("population_union_multi_metric")
}

func expectedComparisonEvidence(scenario string) comparisonSmokeEvidence {
	base := expectedPopulationUnionEvidence()
	switch scenario {
	case "population_union_multi_metric":
		return base
	case "ungrouped_multi_metric_totals":
		decimal := func(value string) *comparison.DecimalString {
			result := comparison.DecimalString(value)
			return &result
		}
		return comparisonSmokeEvidence{
			TimeDimension: base.TimeDimension, Baseline: base.Baseline, Current: base.Current,
			Metrics: append([]string(nil), base.Metrics...), Dimensions: []string{},
			Rows: []comparison.Row{{
				Members: []comparison.Member{}, BaselinePresent: true, CurrentPresent: true,
				Values: []comparison.MetricValue{
					{Metric: "metric:comparison.revenue", BaselineValue: decimal("150"), CurrentValue: decimal("215"), Delta: decimal("65"), PercentChange: decimal("43.333333333333333333"), ChangeDefined: true, PercentDefined: true},
					{Metric: "metric:comparison.sessions", BaselineValue: decimal("17"), CurrentValue: decimal("22"), Delta: decimal("5"), PercentChange: decimal("29.411764705882352941"), ChangeDefined: true, PercentDefined: true},
				},
			}},
		}
	case "zero_baseline_region":
		base.Rows = []comparison.Row{base.Rows[1]}
		return base
	case "null_entry_exit_revenue":
		base.Metrics = []string{"metric:comparison.revenue"}
		base.Rows = append([]comparison.Row(nil), base.Rows[2:]...)
		for index := range base.Rows {
			base.Rows[index].Values = []comparison.MetricValue{base.Rows[index].Values[0]}
		}
		return base
	default:
		panic(fmt.Sprintf("unknown frozen comparison scenario %q", scenario))
	}
}

func expectedPopulationUnionEvidence() comparisonSmokeEvidence {
	periodBaseline := comparison.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"}
	periodCurrent := comparison.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"}
	metricRevenue := "metric:comparison.revenue"
	metricSessions := "metric:comparison.sessions"
	dimension := "dimension:comparison.events.region"
	row := func(region string, baselinePresent, currentPresent bool, values ...comparison.MetricValue) comparison.Row {
		return comparison.Row{Members: []comparison.Member{{Dimension: dimension, MemberType: comparison.MemberString, Value: json.RawMessage(fmt.Sprintf("%q", region))}}, BaselinePresent: baselinePresent, CurrentPresent: currentPresent, Values: values}
	}
	value := func(metric string, baseline, current, delta, percent *string, change, percentDefined bool) comparison.MetricValue {
		convert := func(input *string) *comparison.DecimalString {
			if input == nil {
				return nil
			}
			result := comparison.DecimalString(*input)
			return &result
		}
		return comparison.MetricValue{Metric: metric, BaselineValue: convert(baseline), CurrentValue: convert(current), Delta: convert(delta), PercentChange: convert(percent), ChangeDefined: change, PercentDefined: percentDefined}
	}
	str := func(value string) *string { return &value }
	return comparisonSmokeEvidence{
		TimeDimension: "dimension:comparison.events.event_time", Baseline: periodBaseline, Current: periodCurrent,
		Metrics: []string{metricRevenue, metricSessions}, Dimensions: []string{dimension},
		Rows: []comparison.Row{
			row("east", true, true, value(metricRevenue, str("100"), str("120"), str("20"), str("20"), true, true), value(metricSessions, str("10"), str("12"), str("2"), str("20"), true, true)),
			row("north", true, true, value(metricRevenue, str("0"), str("10"), str("10"), nil, true, false), value(metricSessions, str("0"), str("0"), str("0"), nil, true, false)),
			row("null_value", true, true, value(metricRevenue, nil, str("5"), nil, nil, false, false), value(metricSessions, str("2"), str("2"), str("0"), str("0"), true, true)),
			row("south", true, false, value(metricRevenue, str("50"), nil, nil, nil, false, false), value(metricSessions, str("5"), nil, nil, nil, false, false)),
			row("west", false, true, value(metricRevenue, nil, str("80"), nil, nil, false, false), value(metricSessions, nil, str("8"), nil, nil, false, false)),
		},
	}
}

func newComparisonSmokeRuntime(root string) (*comparisonSmokeRuntime, error) {
	databasePath := filepath.Join(root, "comparison.duckdb")
	if err := seedComparisonDuckDB(databasePath); err != nil {
		return nil, err
	}
	configPath, err := writeComparisonProject(root, databasePath)
	if err != nil {
		return nil, err
	}
	backends, err := backend.NewBackendRegistry(duckdbbackend.New())
	if err != nil {
		return nil, err
	}
	projectRuntime, err := bootstrap.LoadRuntime(configPath, bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		return nil, fmt.Errorf("load comparison runtime: %w", err)
	}
	runtime := &comparisonSmokeRuntime{runtime: projectRuntime, endpoints: map[comparisonSmokeArm]*comparisonSmokeEndpoint{}}
	handlers := map[comparisonSmokeArm]http.Handler{
		comparisonArmQuery:   appmcp.NewObservedHTTPHandlerWithQueryMetrics(projectRuntime.Discovery, projectRuntime.Compile, projectRuntime.QueryMetrics, nil, nil),
		comparisonArmCompare: appmcp.NewObservedHTTPHandlerWithAnalytics(projectRuntime.Discovery, projectRuntime.Compile, projectRuntime.QueryMetrics, nil, projectRuntime.CompareMetrics, nil, nil),
	}
	for _, arm := range []comparisonSmokeArm{comparisonArmQuery, comparisonArmCompare} {
		endpoint, startErr := startComparisonSmokeEndpoint(handlers[arm])
		if startErr != nil {
			runtime.Close(context.Background())
			return nil, startErr
		}
		runtime.endpoints[arm] = endpoint
	}
	return runtime, nil
}

func writeComparisonProject(root, databasePath string) (string, error) {
	if err := os.WriteFile(filepath.Join(root, "comparison.ossie.yaml"), []byte(comparisonModel), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(root, "project.yaml"), []byte("semantic_sources:\n  comparison:\n    path: ./comparison.ossie.yaml\n"), 0o600); err != nil {
		return "", err
	}
	datasources := fmt.Sprintf("duckdb-local:\n  type: duckdb\n  config:\n    path: %q\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1048576\n    max_concurrency: 2\n", databasePath)
	if err := os.WriteFile(filepath.Join(root, "datasources.yaml"), []byte(datasources), 0o600); err != nil {
		return "", err
	}
	deployment := "version: 1\nprojects:\n  " + comparisonProject + ":\n    path: ./project.yaml\n    data_source: duckdb-local\ndata_sources:\n  path: ./datasources.yaml\n"
	path := filepath.Join(root, "metis.yaml")
	if err := os.WriteFile(path, []byte(deployment), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func seedComparisonDuckDB(path string) error {
	connector, err := duckdbdriver.NewConnector(path, nil)
	if err != nil {
		return err
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	statement := `CREATE SCHEMA analytics;
CREATE TABLE analytics.comparison_events(event_time TIMESTAMP, region VARCHAR, revenue DECIMAL(20,4), sessions DECIMAL(20,4));
INSERT INTO analytics.comparison_events VALUES
('2026-07-10','east',100,10),
('2026-07-10','north',0,0),
('2026-07-10','null_value',NULL,2),
('2026-07-10','south',50,5),
('2026-08-10','east',120,12),
('2026-08-10','north',10,0),
('2026-08-10','null_value',5,2),
('2026-08-10','west',80,8);`
	if _, err := database.Exec(statement); err != nil {
		return fmt.Errorf("seed comparison DuckDB: %w", err)
	}
	return nil
}

func startComparisonSmokeEndpoint(handler http.Handler) (*comparisonSmokeEndpoint, error) {
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
		principal := &auth.Principal{TenantID: "s2sbench", SubjectID: "comparison-smoke", APIKeyID: "ephemeral", Scopes: []string{auth.ScopeSemanticRead, auth.ScopeSemanticCompile, auth.ScopeSemanticExecute}}
		handler.ServeHTTP(writer, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
	})
	server := &http.Server{Handler: bearerAuth(token, principalHandler)}
	go func() { _ = server.Serve(listener) }()
	return &comparisonSmokeEndpoint{
		server: server, listener: listener,
		mcp:         s2sbench.AgentMCP{Name: benchmarkMCPServerName, URL: "http://" + listener.Addr().String(), BearerTokenEnvVar: s2sbenchMCPTokenEnv, HTTPHeaders: map[string]string{appmcp.ProjectHeaderKey: comparisonProject}},
		environment: map[string]string{s2sbenchMCPTokenEnv: token},
	}, nil
}

func (runtime *comparisonSmokeRuntime) Close(ctx context.Context) {
	if runtime == nil {
		return
	}
	keys := make([]string, 0, len(runtime.endpoints))
	for arm := range runtime.endpoints {
		keys = append(keys, string(arm))
	}
	sort.Strings(keys)
	for _, key := range keys {
		endpoint := runtime.endpoints[comparisonSmokeArm(key)]
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
