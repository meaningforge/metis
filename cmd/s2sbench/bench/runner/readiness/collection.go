package readiness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/renderer/sql"
)

type Execution interface {
	Prepare(context.Context, scenarios.Scenario) error
	PhysicalSchema(context.Context) (string, error)
	RunSQL(context.Context, string, ...sql.QueryParameter) (scenarios.ResultSet, error)
}

type FileRead struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Bytes     int    `json:"bytes"`
	Order     int    `json:"order"`
}

type AttemptRecord struct {
	Experiment            string                      `json:"experiment"`
	ManifestSchema        string                      `json:"manifest_schema"`
	PromptVersion         string                      `json:"prompt_version"`
	ProjectionVersion     string                      `json:"projection_version"`
	OKFRevision           string                      `json:"okf_revision"`
	Agent                 s2sbench.AgentIdentity      `json:"agent"`
	ModelProvider         string                      `json:"model_provider"`
	ModelID               string                      `json:"model_id"`
	ModelVersion          string                      `json:"model_version"`
	Arm                   Arm                         `json:"arm"`
	Scenario              string                      `json:"scenario"`
	Stratum               s2sbench.Stratum            `json:"stratum"`
	QuestionNumber        int                         `json:"question_number"`
	Repetition            int                         `json:"repetition"`
	Attempt               int                         `json:"attempt"`
	StartedAt             time.Time                   `json:"started_at"`
	DurationMS            int64                       `json:"duration_ms"`
	Output                string                      `json:"output"`
	AnswerStatus          string                      `json:"answer_status,omitempty"`
	SQLFingerprint        string                      `json:"sql_fingerprint,omitempty"`
	Verdict               string                      `json:"verdict"`
	FailureCategory       string                      `json:"failure_category,omitempty"`
	FailureDetail         string                      `json:"failure_detail,omitempty"`
	QueryEvidence         *QueryEvidence              `json:"query_evidence,omitempty"`
	CompileEvidence       *CompileEvidence            `json:"compile_evidence,omitempty"`
	ExecutedQuery         *artifact.CompiledQuery     `json:"executed_query,omitempty"`
	HandoffMatchesCompile bool                        `json:"handoff_matches_compile,omitempty"`
	ParseOK               bool                        `json:"parse_ok"`
	ExecutionOK           bool                        `json:"execution_ok"`
	OracleOK              bool                        `json:"oracle_ok"`
	ToolCalls             int                         `json:"tool_calls"`
	ContextTokens         int                         `json:"context_tokens"`
	OutputTokens          int                         `json:"output_tokens"`
	FileReads             []FileRead                  `json:"file_reads,omitempty"`
	ToolTrace             []s2sbench.ToolCallEvidence `json:"tool_trace,omitempty"`
	WorkspaceTreeDigest   string                      `json:"workspace_tree_digest"`
	FactLedgerDigest      string                      `json:"fact_ledger_digest"`
	SchemaDigest          string                      `json:"schema_digest"`
}

type ArmOutcome struct {
	QuestionNumber             int     `json:"question_number"`
	Scenario                   string  `json:"scenario"`
	Question                   string  `json:"question"`
	Stratum                    string  `json:"stratum"`
	FirstReady                 bool    `json:"first_attempt_ready"`
	SemanticIntentSuccess      bool    `json:"semantic_intent_success"`
	HandoffSuccess             bool    `json:"handoff_success"`
	EndToEndSuccess            bool    `json:"end_to_end_success"`
	Repetitions                int     `json:"repetitions"`
	FirstReadyCount            int     `json:"first_attempt_ready_count"`
	FirstReadyRate             float64 `json:"first_attempt_readiness_rate"`
	SemanticIntentSuccessCount int     `json:"semantic_intent_success_count"`
	SemanticIntentSuccessRate  float64 `json:"semantic_intent_success_rate"`
	HandoffSuccessCount        int     `json:"handoff_success_count"`
	HandoffSuccessRate         float64 `json:"handoff_success_rate"`
	EndToEndSuccessCount       int     `json:"end_to_end_success_count"`
	EndToEndSuccessRate        float64 `json:"end_to_end_success_rate"`
	FinalVerdict               string  `json:"final_verdict"`
}

type Collection struct {
	Manifest         Manifest        `json:"manifest"`
	FactLedgerDigest string          `json:"fact_ledger_digest"`
	WorkspaceDigest  string          `json:"workspace_digest"`
	Attempts         []AttemptRecord `json:"attempts"`
}

type Report struct {
	SchemaVersion             string       `json:"schema_version"`
	Suite                     Suite        `json:"suite"`
	Arm                       Arm          `json:"arm"`
	Questions                 []ArmOutcome `json:"questions"`
	FirstReady                int          `json:"first_attempt_ready"`
	ReadinessRate             float64      `json:"first_attempt_readiness_rate"`
	SemanticIntentSuccess     int          `json:"semantic_intent_success"`
	SemanticIntentSuccessRate float64      `json:"semantic_intent_success_rate"`
	HandoffSuccess            int          `json:"handoff_success"`
	HandoffSuccessRate        float64      `json:"handoff_success_rate"`
	EndToEndSuccess           int          `json:"end_to_end_success"`
	EndToEndSuccessRate       float64      `json:"end_to_end_success_rate"`
	Conclusion                string       `json:"conclusion"`
	Valid                     bool         `json:"valid"`
	InvalidReason             string       `json:"invalid_reason,omitempty"`
}

type AttemptObserver func(AttemptRecord) error

// RequestDecorator adds arm-specific capabilities, such as the benchmark-owned
// Metis MCP endpoint, after the common prompt/workspace/budget are frozen.
type RequestDecorator func(context.Context, Arm, s2sbench.AgentRequest) (s2sbench.AgentRequest, error)

// AttemptDecorator attaches benchmark-owned evidence observed outside the
// Agent protocol, such as the exact CompiledQuery passed to a test Driver.
type AttemptDecorator func(AttemptRecord) (AttemptRecord, error)

func CollectSmoke(ctx context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, projection *Projection, definitions []fixtures.Definition, observer AttemptObserver) (Collection, Report, error) {
	return Collect(ctx, manifest, driver, execution, projection, definitions, observer)
}

func Collect(ctx context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, projection *Projection, definitions []fixtures.Definition, observer AttemptObserver) (Collection, Report, error) {
	return CollectFrom(ctx, manifest, driver, execution, projection, definitions, nil, observer)
}

// CollectFrom resumes a single arm from complete, already journaled question
// repetitions. Completed repetition identities are validated and never invoked again.
func CollectFrom(ctx context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, projection *Projection, definitions []fixtures.Definition, completed []AttemptRecord, observer AttemptObserver) (Collection, Report, error) {
	return CollectFromWithRequestDecorator(ctx, manifest, driver, execution, projection, definitions, completed, observer, nil)
}

func CollectFromWithRequestDecorator(ctx context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, projection *Projection, definitions []fixtures.Definition, completed []AttemptRecord, observer AttemptObserver, decorate RequestDecorator) (Collection, Report, error) {
	return CollectFromWithDecorators(ctx, manifest, driver, execution, projection, definitions, completed, observer, decorate, nil)
}

func CollectFromWithDecorators(ctx context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, projection *Projection, definitions []fixtures.Definition, completed []AttemptRecord, observer AttemptObserver, decorate RequestDecorator, decorateAttempt AttemptDecorator) (Collection, Report, error) {
	if err := manifest.Validate(); err != nil {
		return Collection{}, Report{}, err
	}
	if driver == nil || execution == nil || projection == nil {
		return Collection{}, Report{}, fmt.Errorf("agent driver, execution, and projection are required")
	}
	if err := projection.Validate(); err != nil {
		return Collection{}, Report{}, err
	}
	resolved, err := ResolveScenarios(manifest)
	if err != nil {
		return Collection{}, Report{}, err
	}
	root, err := os.MkdirTemp("", "metis-readiness-")
	if err != nil {
		return Collection{}, Report{}, err
	}
	defer os.RemoveAll(root)
	collection := Collection{
		Manifest:         manifest,
		FactLedgerDigest: projection.Ledger.Digest,
		Attempts:         append([]AttemptRecord(nil), completed...),
	}
	if len(completed) > 0 {
		collection.WorkspaceDigest = completed[0].WorkspaceTreeDigest
	}
	done, err := completedQuestionRepetitions(collection)
	if err != nil {
		return Collection{}, Report{}, err
	}

	for scenarioIndex, scenario := range resolved {
		questionNumber := scenarioIndex + 1
		allRepetitionsDone := true
		for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
			if !done[questionRepetition{QuestionNumber: questionNumber, Repetition: repetition}] {
				allRepetitionsDone = false
				break
			}
		}
		if allRepetitionsDone {
			continue
		}
		arm := manifest.Arm
		if err := execution.Prepare(ctx, scenario); err != nil {
			return collection, Report{}, fmt.Errorf("prepare %s fixture for %q: %w", arm, scenario.Name, err)
		}
		schema, err := execution.PhysicalSchema(ctx)
		if err != nil {
			return collection, Report{}, fmt.Errorf("read physical schema for %q: %w", scenario.Name, err)
		}
		for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
			key := questionRepetition{QuestionNumber: questionNumber, Repetition: repetition}
			if done[key] {
				continue
			}
			workspace := filepath.Join(root, fmt.Sprintf("%02d-r%02d-%s", questionNumber, repetition, arm))
			if err := materializeWorkspace(workspace, arm, schema, projection, definitions); err != nil {
				return collection, Report{}, err
			}
			workspaceDigest, err := TreeDigest(workspace)
			if err != nil {
				return collection, Report{}, err
			}
			if collection.WorkspaceDigest == "" {
				collection.WorkspaceDigest = workspaceDigest
			}
			records, runErr := runQuestion(ctx, manifest, driver, execution, workspace, workspaceDigest, digestString(schema), projection.Ledger.Digest, arm, scenario, manifest.Scenarios[scenarioIndex], questionNumber, repetition, decorate)
			if runErr != nil {
				return collection, Report{}, runErr
			}
			for _, record := range records {
				if decorateAttempt != nil {
					record, err = decorateAttempt(record)
					if err != nil {
						return collection, Report{}, err
					}
				}
				collection.Attempts = append(collection.Attempts, record)
				if observer != nil {
					if err := observer(record); err != nil {
						return collection, Report{}, err
					}
				}
			}
		}
	}
	report, err := BuildReport(collection)
	return collection, report, err
}

type questionRepetition struct {
	QuestionNumber int
	Repetition     int
}

func completedQuestionRepetitions(collection Collection) (map[questionRepetition]bool, error) {
	if _, err := BuildReport(collection); err != nil {
		return nil, fmt.Errorf("validate resumed readiness evidence: %w", err)
	}
	byQuestion := make(map[questionRepetition][]AttemptRecord, len(collection.Manifest.Scenarios)*collection.Manifest.Budget.Repetitions)
	for _, attempt := range collection.Attempts {
		key := questionRepetition{QuestionNumber: attempt.QuestionNumber, Repetition: attempt.Repetition}
		byQuestion[key] = append(byQuestion[key], attempt)
	}
	done := make(map[questionRepetition]bool, len(byQuestion))
	for key, attempts := range byQuestion {
		last := attempts[0]
		for _, attempt := range attempts[1:] {
			if attempt.Attempt > last.Attempt {
				last = attempt
			}
		}
		terminal := last.Verdict == "correct" || last.Attempt == collection.Manifest.Budget.Attempts || last.FailureCategory == "timeout" || last.FailureCategory == "tool_budget_exceeded"
		if !terminal {
			return nil, fmt.Errorf("question %d repetition %d has an incomplete attempt batch; refusing to resume with a new repair session", key.QuestionNumber, key.Repetition)
		}
		done[key] = true
	}
	return done, nil
}

func runQuestion(parent context.Context, manifest Manifest, driver s2sbench.AgentDriver, execution Execution, workspace, workspaceDigest, schemaDigest, ledgerDigest string, arm Arm, scenario scenarios.Scenario, spec ScenarioSpec, position, repetition int, decorate RequestDecorator) ([]AttemptRecord, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(manifest.Budget.QuestionTimeoutMS)*time.Millisecond)
	defer cancel()
	remainingCalls := manifest.Budget.ToolCalls
	failure := ""
	var session s2sbench.AgentSession
	var records []AttemptRecord
	finish := func() []AttemptRecord {
		return finalizeCompileEvidence(parent, execution, scenario, manifest.Target.Dialect, records)
	}
	defer func() {
		if session != nil {
			_ = session.Close()
		}
	}()
	for attemptIndex := 1; attemptIndex <= manifest.Budget.Attempts; attemptIndex++ {
		budget := manifest.Budget
		budget.ToolCalls = remainingCalls
		request := s2sbench.AgentRequest{Workspace: workspace, Prompt: TurnPromptForArm(spec.Question, failure, manifest.Budget, manifest.Target, arm), Budget: budget}
		if decorate != nil {
			var err error
			request, err = decorate(ctx, arm, request)
			if err != nil {
				return records, fmt.Errorf("build %s agent request for %q: %w", arm, scenario.Name, err)
			}
		}
		if session == nil {
			var err error
			session, err = driver.Open(ctx, request)
			if err != nil {
				return records, fmt.Errorf("open %s agent session for %q: %w", arm, scenario.Name, err)
			}
		}
		started := time.Now().UTC()
		result, agentErr := session.Run(ctx, request)
		record := baseAttemptRecord(manifest, arm, scenario, spec, position, repetition, attemptIndex, started, workspace, workspaceDigest, schemaDigest, ledgerDigest, result)
		record.DurationMS = time.Since(started).Milliseconds()
		remainingCalls -= result.ToolCalls
		if remainingCalls >= 0 && !s2sbench.IsToolBudgetExceededError(agentErr) && arm == ArmMetisMCP {
			if scored, observed := scoreCapturedQueryMetrics(scenario, record); observed {
				records = append(records, scored)
				if scored.Verdict == "correct" {
					return finish(), nil
				}
				failure = "The structured query result did not satisfy the executable result contract."
				continue
			}
			if scored, observed := scoreCapturedCompile(parent, execution, scenario, manifest.Target.Dialect, record); observed {
				records = append(records, scored)
				if scored.Verdict == "correct" {
					return finish(), nil
				}
				failure = "The structured compile result did not satisfy the executable result contract."
				continue
			}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			record.Verdict = "failed"
			record.FailureCategory = "timeout"
			record.FailureDetail = boundedFailureDetail(ctx.Err())
			record.ToolCalls = result.ToolCalls
			record.ContextTokens = result.ContextTokens
			record.OutputTokens = result.OutputTokens
			record.Output = strings.TrimSpace(result.Output)
			record.ToolTrace = append([]s2sbench.ToolCallEvidence(nil), result.ToolTrace...)
			records = append(records, record)
			return finish(), nil
		}
		if remainingCalls < 0 || s2sbench.IsToolBudgetExceededError(agentErr) {
			record.Verdict = "failed"
			record.FailureCategory = "tool_budget_exceeded"
			record.FailureDetail = boundedFailureDetail(agentErr)
			records = append(records, record)
			return finish(), nil
		}
		if agentErr != nil {
			record.Verdict = "failed"
			record.FailureDetail = boundedFailureDetail(agentErr)
			if errors.Is(agentErr, context.DeadlineExceeded) || s2sbench.IsAgentTurnTimeoutError(agentErr) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				record.FailureCategory = "timeout"
				records = append(records, record)
				return finish(), nil
			}
			if s2sbench.IsAgentUnavailableError(agentErr) {
				record.FailureCategory = "agent_unavailable"
				records = append(records, record)
				return records, fmt.Errorf("external agent unavailable during %s/%s: %w", arm, scenario.Name, agentErr)
			}
			record.FailureCategory = "agent_error"
			records = append(records, record)
			failure = "The Agent turn failed before a valid answer was returned."
			continue
		}

		answer, decodeErr := DecodeAnswerForDialect(result.Output, manifest.Target.Dialect)
		if decodeErr != nil {
			record.Verdict = "failed"
			record.FailureCategory = "malformed_answer"
			record.FailureDetail = boundedFailureDetail(decodeErr)
			records = append(records, record)
			failure = "The answer envelope was malformed or the SQL was not one read-only DuckDB statement."
			continue
		}
		record.ParseOK = true
		record.AnswerStatus = answer.Status
		if answer.Status == "not_ready" {
			record.Verdict = "failed"
			record.FailureCategory = "unexpected_not_ready"
			record.FailureDetail = boundedFailureDetail(errors.New(answer.Reason))
			records = append(records, record)
			failure = "This positive scenario received a not_ready answer."
			continue
		}
		record.SQLFingerprint = SQLFingerprint(answer.SQL, answer.Parameters...)
		actual, executionErr := execution.RunSQL(ctx, answer.SQL, answer.Parameters...)
		if executionErr != nil {
			record.Verdict = "failed"
			record.FailureDetail = boundedFailureDetail(executionErr)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(executionErr, context.DeadlineExceeded) {
				record.FailureCategory = "timeout"
				records = append(records, record)
				return finish(), nil
			}
			record.FailureCategory = "sql_execution_failed"
			records = append(records, record)
			failure = "The read-only SQL did not parse or execute successfully."
			continue
		}
		record.ExecutionOK = true
		verdict, judgeErr := s2sbench.Judge(scenario, actual)
		if verdict == s2sbench.VerdictCorrect && judgeErr == nil {
			record.OracleOK = true
			record.Verdict = "correct"
			records = append(records, record)
			return finish(), nil
		}
		record.Verdict = "silent_wrong"
		record.FailureCategory = resultMismatchCategory(judgeErr)
		record.FailureDetail = boundedFailureDetail(judgeErr)
		records = append(records, record)
		if record.FailureCategory == "result_type_mismatch" {
			failure = "The SQL executed, but its normalized result column types did not match the semantic result contract."
		} else {
			failure = "The SQL executed, but its normalized result values did not match the semantic result contract."
		}
	}
	return finish(), nil
}

func boundedFailureDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := strings.TrimSpace(err.Error())
	const maximum = 2048
	if len(detail) > maximum {
		detail = detail[:maximum]
	}
	return detail
}

func resultMismatchCategory(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "column ") && strings.Contains(err.Error(), ", want ") {
		return "result_type_mismatch"
	}
	return "result_value_mismatch"
}

func baseAttemptRecord(manifest Manifest, arm Arm, scenario scenarios.Scenario, spec ScenarioSpec, position, repetition, attempt int, started time.Time, workspace, workspaceDigest, schemaDigest, ledgerDigest string, result s2sbench.AgentResult) AttemptRecord {
	fileReads := extractFileReads(result.ToolTrace)
	fileReads = append(fileReads, extractTranscriptFileReads(result.Transcript, workspace, len(fileReads))...)
	return AttemptRecord{
		Experiment: manifest.Experiment, ManifestSchema: manifest.SchemaVersion, PromptVersion: manifest.PromptVersion,
		ProjectionVersion: manifest.ProjectionVersion, OKFRevision: manifest.OKFRevision,
		Agent: manifest.Agent, ModelProvider: manifest.ModelProvider, ModelID: manifest.ModelID, ModelVersion: manifest.ModelVersion,
		Arm: arm, Scenario: scenario.Name, Stratum: spec.Stratum, QuestionNumber: position, Repetition: repetition, Attempt: attempt, StartedAt: started,
		Output: strings.TrimSpace(result.Output), ToolCalls: result.ToolCalls, ContextTokens: result.ContextTokens, OutputTokens: result.OutputTokens,
		FileReads: fileReads, ToolTrace: append([]s2sbench.ToolCallEvidence(nil), result.ToolTrace...),
		WorkspaceTreeDigest: workspaceDigest, FactLedgerDigest: ledgerDigest, SchemaDigest: schemaDigest,
	}
}

func BuildReport(collection Collection) (Report, error) {
	if err := collection.Manifest.Validate(); err != nil {
		return Report{}, err
	}
	expectedQuestions := make(map[string]int, len(collection.Manifest.Scenarios))
	seenAttempts := make(map[questionRepetition]map[int]bool, len(collection.Manifest.Scenarios)*collection.Manifest.Budget.Repetitions)
	for index, spec := range collection.Manifest.Scenarios {
		expectedQuestions[spec.Name] = index + 1
	}
	for _, attempt := range collection.Attempts {
		if attempt.Arm != collection.Manifest.Arm {
			return Report{}, fmt.Errorf("attempt for arm %q found in %q single-arm collection", attempt.Arm, collection.Manifest.Arm)
		}
		questionNumber, ok := expectedQuestions[attempt.Scenario]
		if !ok {
			return Report{}, fmt.Errorf("attempt references unknown scenario %q", attempt.Scenario)
		}
		if attempt.QuestionNumber != questionNumber {
			return Report{}, fmt.Errorf("scenario %q has question number %d, want %d", attempt.Scenario, attempt.QuestionNumber, questionNumber)
		}
		if attempt.Repetition < 1 || attempt.Repetition > collection.Manifest.Budget.Repetitions {
			return Report{}, fmt.Errorf("scenario %q has invalid repetition %d", attempt.Scenario, attempt.Repetition)
		}
		if attempt.Attempt < 1 || attempt.Attempt > collection.Manifest.Budget.Attempts {
			return Report{}, fmt.Errorf("scenario %q has invalid attempt number %d", attempt.Scenario, attempt.Attempt)
		}
		key := questionRepetition{QuestionNumber: attempt.QuestionNumber, Repetition: attempt.Repetition}
		if seenAttempts[key] == nil {
			seenAttempts[key] = map[int]bool{}
		}
		if seenAttempts[key][attempt.Attempt] {
			return Report{}, fmt.Errorf("scenario %q repetition %d repeats attempt number %d", attempt.Scenario, attempt.Repetition, attempt.Attempt)
		}
		seenAttempts[key][attempt.Attempt] = true
	}
	report := Report{SchemaVersion: "s2sbench-arm-report-v2", Suite: collection.Manifest.Suite, Arm: collection.Manifest.Arm, Conclusion: string(collection.Manifest.Suite) + "_single_arm_only", Valid: true}
	for questionIndex, spec := range collection.Manifest.Scenarios {
		questionNumber := questionIndex + 1
		outcome := ArmOutcome{QuestionNumber: questionNumber, Scenario: spec.Name, Question: spec.Question, Stratum: string(spec.Stratum), Repetitions: collection.Manifest.Budget.Repetitions}
		for repetition := 1; repetition <= collection.Manifest.Budget.Repetitions; repetition++ {
			var repetitionAttempts []AttemptRecord
			for _, attempt := range collection.Attempts {
				if attempt.Scenario == spec.Name && attempt.Repetition == repetition {
					repetitionAttempts = append(repetitionAttempts, attempt)
				}
			}
			if len(repetitionAttempts) == 0 {
				report.Valid = false
				report.InvalidReason = "missing scored question repetition evidence"
				continue
			}
			finalAttempt := repetitionAttempts[0]
			semanticSuccess := false
			handoffSuccess := false
			for _, attempt := range repetitionAttempts {
				if attempt.Attempt == 1 && attempt.Verdict == "correct" {
					outcome.FirstReadyCount++
					report.FirstReady++
				}
				if attempt.Attempt > finalAttempt.Attempt {
					finalAttempt = attempt
				}
				semanticSuccess = semanticSuccess || attempt.OracleOK || (attempt.QueryEvidence != nil && attempt.QueryEvidence.OracleOK) || (attempt.CompileEvidence != nil && attempt.CompileEvidence.OracleOK)
				handoffSuccess = handoffSuccess || attempt.ParseOK
			}
			if semanticSuccess {
				outcome.SemanticIntentSuccessCount++
				report.SemanticIntentSuccess++
			}
			if handoffSuccess {
				outcome.HandoffSuccessCount++
				report.HandoffSuccess++
			}
			if finalAttempt.Verdict == "correct" {
				outcome.EndToEndSuccessCount++
				report.EndToEndSuccess++
			}
		}
		denominator := float64(outcome.Repetitions)
		outcome.FirstReadyRate = float64(outcome.FirstReadyCount) / denominator
		outcome.SemanticIntentSuccessRate = float64(outcome.SemanticIntentSuccessCount) / denominator
		outcome.HandoffSuccessRate = float64(outcome.HandoffSuccessCount) / denominator
		outcome.EndToEndSuccessRate = float64(outcome.EndToEndSuccessCount) / denominator
		outcome.FirstReady = outcome.FirstReadyCount == outcome.Repetitions
		outcome.SemanticIntentSuccess = outcome.SemanticIntentSuccessCount == outcome.Repetitions
		outcome.HandoffSuccess = outcome.HandoffSuccessCount == outcome.Repetitions
		outcome.EndToEndSuccess = outcome.EndToEndSuccessCount == outcome.Repetitions
		switch {
		case outcome.EndToEndSuccessCount == outcome.Repetitions:
			outcome.FinalVerdict = "correct"
		case outcome.EndToEndSuccessCount == 0:
			outcome.FinalVerdict = "failed"
		default:
			outcome.FinalVerdict = "mixed"
		}
		report.Questions = append(report.Questions, outcome)
	}
	if total := len(report.Questions) * collection.Manifest.Budget.Repetitions; total > 0 {
		report.ReadinessRate = float64(report.FirstReady) / float64(total)
		report.SemanticIntentSuccessRate = float64(report.SemanticIntentSuccess) / float64(total)
		report.HandoffSuccessRate = float64(report.HandoffSuccess) / float64(total)
		report.EndToEndSuccessRate = float64(report.EndToEndSuccess) / float64(total)
	}
	controlTotal, controlReady := 0, 0
	for _, outcome := range report.Questions {
		if outcome.Stratum != string(s2sbench.StratumControl) {
			continue
		}
		controlTotal += outcome.Repetitions
		controlReady += outcome.FirstReadyCount
	}
	if controlTotal > 0 && controlReady*10 < controlTotal*7 {
		report.Valid = false
		report.InvalidReason = "control-stratum first-attempt correctness is below the frozen 70% validity gate"
	}
	return report, nil
}

func materializeWorkspace(root string, arm Arm, schema string, projection *Projection, definitions []fixtures.Definition) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	switch arm {
	case ArmOKF:
		if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(schema), 0o400); err != nil {
			return err
		}
		if err := projection.WriteBundle(filepath.Join(root, "knowledge")); err != nil {
			return err
		}
	case ArmMetisMCP:
		// Semantic assets are supplied through MCP for this arm.
	default:
		return fmt.Errorf("unknown S2SBench arm %q", arm)
	}
	return nil
}

func extractFileReads(trace []s2sbench.ToolCallEvidence) []FileRead {
	var reads []FileRead
	for index, call := range trace {
		name := strings.ToLower(call.Name)
		if !strings.Contains(name, "read") && !strings.Contains(name, "open") && !strings.Contains(name, "exec") && !strings.Contains(name, "shell") {
			continue
		}
		path := extractPath(call.Arguments)
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			continue
		}
		reads = append(reads, FileRead{Path: filepath.ToSlash(filepath.Clean(path)), Operation: call.Name, Bytes: call.ResponseBytes, Order: index + 1})
	}
	return reads
}

func extractTranscriptFileReads(transcript []byte, workspace string, orderOffset int) []FileRead {
	var reads []FileRead
	for _, line := range strings.Split(string(transcript), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Item map[string]any `json:"item"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &message) != nil || message.Method != "item/completed" || message.Params.Item["type"] != "commandExecution" {
			continue
		}
		command := commandText(message.Params.Item["command"])
		if command == "" {
			continue
		}
		responseBytes := len(anyString(message.Params.Item["aggregatedOutput"]))
		if responseBytes == 0 {
			responseBytes = len(anyString(message.Params.Item["output"]))
		}
		operation := firstCommandWord(command)
		seen := map[string]struct{}{}
		for _, candidate := range commandPathCandidates(command) {
			relative := workspaceRelativePath(workspace, candidate)
			if relative == "" {
				continue
			}
			if _, duplicate := seen[relative]; duplicate {
				continue
			}
			seen[relative] = struct{}{}
			reads = append(reads, FileRead{Path: relative, Operation: operation, Bytes: responseBytes, Order: len(reads) + orderOffset + 1})
		}
	}
	return reads
}

func commandText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func anyString(value any) string {
	text, _ := value.(string)
	return text
}

func firstCommandWord(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "commandExecution"
	}
	return strings.Trim(fields[0], "'\"")
}

func commandPathCandidates(command string) []string {
	replacer := strings.NewReplacer("|", " ", ";", " ", "&&", " ", "||", " ", "(", " ", ")", " ", ">", " ", "<", " ")
	fields := strings.Fields(replacer.Replace(command))
	var candidates []string
	for _, field := range fields {
		field = strings.Trim(field, "'\"`,")
		if field == "" || strings.HasPrefix(field, "-") || strings.Contains(field, "=") || strings.Contains(field, "*") {
			continue
		}
		candidates = append(candidates, field)
	}
	return candidates
}

func workspaceRelativePath(workspace, candidate string) string {
	path := candidate
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, filepath.FromSlash(path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	if _, err := os.Stat(absolute); err != nil {
		return ""
	}
	return filepath.ToSlash(relative)
}

func extractPath(arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	var search func(any) string
	search = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				lower := strings.ToLower(key)
				if lower == "path" || lower == "file_path" || lower == "filepath" {
					if text, ok := typed[key].(string); ok {
						return text
					}
				}
			}
			for _, key := range keys {
				if found := search(typed[key]); found != "" {
					return found
				}
			}
		case []any:
			for _, item := range typed {
				if found := search(item); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return search(value)
}

func SchemaDigest(schema string) string {
	sum := sha256.Sum256([]byte(schema))
	return "sha256:" + hex.EncodeToString(sum[:])
}
