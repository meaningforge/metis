// Package readiness implements S2SBench's frozen semantic-interface frame.
package readiness

import (
	"fmt"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/query"
)

const (
	ManifestSchemaVersion          = "s2sbench-semantic-interface-v2"
	PromptVersion                  = "s2sbench-semantic-interface-prompt-v2"
	ProjectionVersion              = "source-catalog-to-okf-v1"
	CatalogProjectionVersion       = ProjectionVersion
	OKFVersion                     = "0.2"
	OKFRevision                    = "ad30107c31c06aec8a7d5636e0d1058118604e6f"
	AnalysisSeed             int64 = 680068
)

var registeredArms = map[Arm]struct{}{
	ArmOKF: {}, ArmMetisMCP: {},
}

type Arm string

const (
	ArmOKF      Arm = "okf"
	ArmMetisMCP Arm = "metis-mcp"
)

func ParseArm(value string) (Arm, error) {
	arm := Arm(strings.TrimSpace(value))
	if _, ok := registeredArms[arm]; ok {
		return arm, nil
	}
	return "", fmt.Errorf("arm %q is not supported; want %q or %q", value, ArmOKF, ArmMetisMCP)
}

type Suite string

const (
	SuiteSmoke          Suite = "smoke"
	SuiteDiagnostic     Suite = "diagnostic"
	SuiteFormal         Suite = "formal"
	SuiteCumulativeCase Suite = "cumulative-case"
	SuiteFiltersCase    Suite = "filters-case"
)

var registeredSuites = map[Suite][]ScenarioSpec{
	SuiteSmoke:          SmokeScenarios,
	SuiteDiagnostic:     DiagnosticScenarios,
	SuiteFormal:         FormalPositiveScenarios,
	SuiteCumulativeCase: CumulativeCaseScenarios,
	SuiteFiltersCase:    FiltersCaseScenarios,
}

func ParseSuite(value string) (Suite, error) {
	suite := Suite(strings.TrimSpace(value))
	if _, err := ScenariosForSuite(suite); err != nil {
		return "", err
	}
	return suite, nil
}

type ScenarioSpec struct {
	Name           string                       `json:"name"`
	Stratum        s2sbench.Stratum             `json:"stratum"`
	Question       string                       `json:"question"`
	Query          *query.SemanticQuery         `json:"query,omitempty"`
	ExpectedResult *scenarios.ResultExpectation `json:"expected_result,omitempty"`
}

type Target struct {
	Engine  string `json:"engine"`
	Dialect string `json:"dialect"`
}

var DuckDBTarget = Target{Engine: "duckdb", Dialect: "DUCKDB"}

// SmokeScenarios are frozen before the first live smoke. The first scenario
// exercises ordinary aggregation and the second exercises semantic-risk
// cumulative behavior.
var SmokeScenarios = []ScenarioSpec{
	{Name: "aggregation_variants", Stratum: s2sbench.StratumControl, Question: "What are the average, smallest, and largest order amounts?"},
	{Name: "cumulative_metric_by_month", Stratum: s2sbench.StratumSilentSemantics, Question: "Show revenue to date for each month."},
}

// DiagnosticScenarios are a frozen one-per-stratum probe. Diagnostic evidence
// is intentionally excluded from the future formal 30-positive/6-negative
// collection and cannot be pooled with smoke or formal evidence.
var DiagnosticScenarios = []ScenarioSpec{
	{Name: "aggregation_variants", Stratum: s2sbench.StratumControl, Question: "What are the average, smallest, and largest order amounts?"},
	{Name: "filters_order_limit", Stratum: s2sbench.StratumEdge, Question: "Revenue by order status for paid orders placed during 2026, highest revenue first, breaking ties by status alphabetically, at most 25 rows."},
	{Name: "multiple_metrics_joined_dimension", Stratum: s2sbench.StratumFanout, Question: "For each customer region, what are total revenue and order count?"},
	{Name: "cumulative_metric_by_month", Stratum: s2sbench.StratumSilentSemantics, Question: "Show revenue to date for each month."},
}

// FormalPositiveScenarios freezes the first formal positive frame before any
// formal live collection. The audited canonical corpus contains only five
// unique edge scenarios, so the preregistered 10/8/6/6 allocation was amended
// before collection to 10/8/5/6 rather than duplicating or backfilling a case.
var FormalPositiveScenarios = []ScenarioSpec{
	// silent_semantics (10)
	{Name: "conversion_count_filtered_campaign", Stratum: s2sbench.StratumSilentSemantics, Question: "For campaign A, how many people who signed up went on to purchase?"},
	{Name: "cumulative_metric_by_quarter", Stratum: s2sbench.StratumSilentSemantics, Question: "Show revenue to date for each quarter."},
	{Name: "custom_calendar_cumulative_with_filter", Stratum: s2sbench.StratumSilentSemantics, Question: "For each fiscal week, show rolling three-fiscal-week revenue only where it is greater than 10."},
	{Name: "custom_calendar_rolling_three_fiscal_weeks", Stratum: s2sbench.StratumSilentSemantics, Question: "For each fiscal week, what was revenue over that fiscal week and the two preceding fiscal weeks?"},
	{Name: "metric_filter_cumulative_metric", Stratum: s2sbench.StratumSilentSemantics, Question: "For each month, show revenue to date only where it is greater than 300."},
	{Name: "semi_additive_as_of_time", Stratum: s2sbench.StratumSilentSemantics, Question: "For each warehouse, what was the inventory balance as of 30 June 2026?"},
	{Name: "semi_additive_last_skip_null", Stratum: s2sbench.StratumSilentSemantics, Question: "For each warehouse, what is the latest recorded inventory level, ignoring days with no reading?"},
	{Name: "semi_additive_since_time", Stratum: s2sbench.StratumSilentSemantics, Question: "For each warehouse, what is the latest inventory balance among readings since 1 January 2026?"},
	{Name: "semi_additive_with_ordinary_metric", Stratum: s2sbench.StratumSilentSemantics, Question: "For each warehouse, what are the inventory balance and total inventory quantity?"},
	{Name: "time_offset_previous_year", Stratum: s2sbench.StratumSilentSemantics, Question: "For each year, what was revenue in the previous year?"},

	// fanout (8)
	{Name: "derived_metric", Stratum: s2sbench.StratumFanout, Question: "What is contribution margin overall?"},
	{Name: "independent_multi_source_ungrouped", Stratum: s2sbench.StratumFanout, Question: "What are total revenue and total cost overall?"},
	{Name: "joined_ratio_repeated_entities", Stratum: s2sbench.StratumFanout, Question: "For each customer region, what is revenue per customer?"},
	{Name: "metric_filter_zero_and_negative_groups", Stratum: s2sbench.StratumFanout, Question: "Revenue by order status, retaining only statuses whose revenue is zero or negative."},
	{Name: "multi_hop_join", Stratum: s2sbench.StratumFanout, Question: "Revenue by country."},
	{Name: "nested_derived_metric", Stratum: s2sbench.StratumFanout, Question: "What is contribution margin as a share of revenue overall?"},
	{Name: "ratio_metric", Stratum: s2sbench.StratumFanout, Question: "What is average order value overall?"},
	{Name: "temporal_join_point_in_time", Stratum: s2sbench.StratumFanout, Question: "How much revenue is associated with each customer tier as it applied at the time of each order?"},

	// edge (5)
	{Name: "grouping_null_dimension", Stratum: s2sbench.StratumEdge, Question: "Revenue and order count by order status, including orders whose status is missing."},
	{Name: "distinct_dimension_values", Stratum: s2sbench.StratumEdge, Question: "Which regions do we operate in within Japan? List them alphabetically, at most 10."},
	{Name: "filters_order_limit", Stratum: s2sbench.StratumEdge, Question: "Revenue by order status for paid orders placed during 2026, highest revenue first, breaking ties by status alphabetically, at most 25 rows."},
	{Name: "order_by_metric_ungrouped", Stratum: s2sbench.StratumEdge, Question: "What is total revenue, sorted from highest to lowest?"},
	{Name: "ordered_ties_secondary_key", Stratum: s2sbench.StratumEdge, Question: "Revenue by order status, excluding pending orders, highest revenue first, breaking ties by status alphabetically, top 3."},

	// control (6)
	{Name: "aggregation_null_zero_negative", Stratum: s2sbench.StratumControl, Question: "What are the average, smallest, and largest order amounts?"},
	{Name: "limit_without_order_by", Stratum: s2sbench.StratumControl, Question: "Revenue by order status, at most 5 rows."},
	{Name: "multiple_local_dimensions", Stratum: s2sbench.StratumControl, Question: "Revenue by order status and order date."},
	{Name: "multiple_metrics_same_source", Stratum: s2sbench.StratumControl, Question: "What are total revenue and order count overall?"},
	{Name: "multiple_metrics_time_grain", Stratum: s2sbench.StratumControl, Question: "What are monthly revenue and order count?"},
	{Name: "time_filter_inclusive_boundaries", Stratum: s2sbench.StratumControl, Question: "Monthly revenue from 1 January through 1 February 2026, inclusive."},
}

// FormalPriorSampleOverlap declares every unavoidable overlap with the frozen
// S2SBench v0 sample. The edge stratum has only five canonical scenarios;
// four already belong to v0 and cannot be replaced without inventing cases.
var FormalPriorSampleOverlap = []string{
	"distinct_dimension_values",
	"filters_order_limit",
	"order_by_metric_ungrouped",
	"ordered_ties_secondary_key",
}

// CumulativeCaseScenarios is the single-pair diagnostic used to reproduce the
// cumulative case without spending calls on the other diagnostic strata.
var CumulativeCaseScenarios = []ScenarioSpec{
	{Name: "cumulative_metric_by_month", Stratum: s2sbench.StratumSilentSemantics, Question: "Show revenue to date for each month."},
}

var FiltersCaseScenarios = []ScenarioSpec{
	{Name: "filters_order_limit", Stratum: s2sbench.StratumEdge, Question: "Revenue by order status for paid orders placed during 2026, highest revenue first, breaking ties by status alphabetically, at most 25 rows."},
}

var FrozenBudget = s2sbench.TaskBudget{
	ToolCalls:         30,
	Attempts:          2,
	QuestionTimeoutMS: 60_000,
	Repetitions:       1,
}

var FormalBudget = s2sbench.TaskBudget{
	ToolCalls:         FrozenBudget.ToolCalls,
	Attempts:          FrozenBudget.Attempts,
	QuestionTimeoutMS: FrozenBudget.QuestionTimeoutMS,
	Repetitions:       5,
}

type Manifest struct {
	SchemaVersion      string                      `json:"schema_version"`
	Experiment         string                      `json:"experiment"`
	Suite              Suite                       `json:"suite"`
	SuiteDigest        string                      `json:"suite_digest,omitempty"`
	Arm                Arm                         `json:"arm"`
	PromptVersion      string                      `json:"prompt_version"`
	ProjectionVersion  string                      `json:"projection_version"`
	OKFVersion         string                      `json:"okf_version"`
	OKFRevision        string                      `json:"okf_revision"`
	Agent              s2sbench.AgentIdentity      `json:"agent"`
	ModelProvider      string                      `json:"model_provider"`
	ModelID            string                      `json:"model_id"`
	ModelVersion       string                      `json:"model_version"`
	ProjectContextMode s2sbench.ProjectContextMode `json:"project_context_mode"`
	Target             Target                      `json:"target"`
	MaterialSource     string                      `json:"material_source,omitempty"`
	MaterialDigest     string                      `json:"material_digest,omitempty"`
	Budget             s2sbench.TaskBudget         `json:"budget"`
	Scenarios          []ScenarioSpec              `json:"scenarios"`
	AnalysisSeed       int64                       `json:"analysis_seed"`
	BootstrapResamples int                         `json:"bootstrap_resamples"`
	EquivalenceMargin  float64                     `json:"equivalence_margin"`
	PriorSampleOverlap []string                    `json:"prior_sample_overlap,omitempty"`
}

func NewSmokeManifest(arm Arm, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifest(SuiteSmoke, arm, nil, agent, provider, modelID, modelVersion)
}

func NewDiagnosticManifest(arm Arm, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifest(SuiteDiagnostic, arm, nil, agent, provider, modelID, modelVersion)
}

func NewFormalPositiveManifest(arm Arm, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifest(SuiteFormal, arm, nil, agent, provider, modelID, modelVersion)
}

func NewCumulativeCaseManifest(arm Arm, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifest(SuiteCumulativeCase, arm, nil, agent, provider, modelID, modelVersion)
}

func NewFiltersCaseManifest(arm Arm, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifest(SuiteFiltersCase, arm, nil, agent, provider, modelID, modelVersion)
}

func NewManifest(suite Suite, arm Arm, scenarioNames []string, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string) (Manifest, error) {
	return NewManifestForTarget(suite, arm, scenarioNames, agent, provider, modelID, modelVersion, DuckDBTarget)
}

func NewManifestForTarget(suite Suite, arm Arm, scenarioNames []string, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string, target Target) (Manifest, error) {
	available, err := ScenariosForSuite(suite)
	if err != nil {
		return Manifest{}, err
	}
	return newManifestFromScenarios(suite, "", arm, available, scenarioNames, agent, provider, modelID, modelVersion, target)
}

func NewWorkloadManifest(suite Suite, suiteDigest string, available []ScenarioSpec, arm Arm, scenarioNames []string, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string, target Target) (Manifest, error) {
	if strings.TrimSpace(suiteDigest) == "" {
		return Manifest{}, fmt.Errorf("custom suite digest is required")
	}
	return newManifestFromScenarios(suite, suiteDigest, arm, available, scenarioNames, agent, provider, modelID, modelVersion, target)
}

func newManifestFromScenarios(suite Suite, suiteDigest string, arm Arm, available []ScenarioSpec, scenarioNames []string, agent s2sbench.AgentIdentity, provider, modelID, modelVersion string, target Target) (Manifest, error) {
	selected := available
	if len(scenarioNames) > 0 {
		requested := make(map[string]struct{}, len(scenarioNames))
		for _, raw := range scenarioNames {
			name := strings.TrimSpace(raw)
			if name == "" {
				return Manifest{}, fmt.Errorf("scenario name is required")
			}
			if _, duplicate := requested[name]; duplicate {
				return Manifest{}, fmt.Errorf("scenario %q is selected more than once", name)
			}
			requested[name] = struct{}{}
		}
		selected = nil
		for _, spec := range available {
			if _, ok := requested[spec.Name]; ok {
				selected = append(selected, spec)
				delete(requested, spec.Name)
			}
		}
		for unknown := range requested {
			return Manifest{}, fmt.Errorf("scenario %q is not in suite %q", unknown, suite)
		}
	}
	manifest := Manifest{
		SchemaVersion:      ManifestSchemaVersion,
		Experiment:         "s2sbench",
		Suite:              suite,
		SuiteDigest:        strings.TrimSpace(suiteDigest),
		Arm:                arm,
		PromptVersion:      PromptVersion,
		ProjectionVersion:  ProjectionVersion,
		OKFVersion:         OKFVersion,
		OKFRevision:        OKFRevision,
		Agent:              agent,
		ModelProvider:      strings.TrimSpace(provider),
		ModelID:            strings.TrimSpace(modelID),
		ModelVersion:       strings.TrimSpace(modelVersion),
		ProjectContextMode: s2sbench.ProjectContextConfigured,
		Target:             Target{Engine: strings.TrimSpace(target.Engine), Dialect: strings.ToUpper(strings.TrimSpace(target.Dialect))},
		Budget:             budgetForSuite(suite),
		Scenarios:          append([]ScenarioSpec(nil), selected...),
		AnalysisSeed:       AnalysisSeed,
		BootstrapResamples: 10_000,
		EquivalenceMargin:  0.05,
	}
	if suite == SuiteFormal {
		manifest.PriorSampleOverlap = append([]string(nil), FormalPriorSampleOverlap...)
	}
	return manifest, manifest.Validate()
}

func ScenariosForSuite(suite Suite) ([]ScenarioSpec, error) {
	selected, ok := registeredSuites[suite]
	if !ok {
		return nil, fmt.Errorf("suite %q is not supported", suite)
	}
	return append([]ScenarioSpec(nil), selected...), nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion || m.Experiment != "s2sbench" {
		return fmt.Errorf("manifest does not describe the frozen S2SBench frame")
	}
	if _, err := ParseArm(string(m.Arm)); err != nil {
		return err
	}
	if m.PromptVersion != PromptVersion || m.ProjectionVersion != CatalogProjectionVersion || m.OKFVersion != OKFVersion || m.OKFRevision != OKFRevision {
		return fmt.Errorf("manifest representation identity differs from the frozen frame")
	}
	if err := m.Agent.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(m.ModelProvider) == "" || strings.TrimSpace(m.ModelID) == "" || strings.TrimSpace(m.ModelVersion) == "" {
		return fmt.Errorf("provider, model ID, and model version are required")
	}
	if strings.TrimSpace(m.Agent.Model) != strings.TrimSpace(m.ModelID) {
		return fmt.Errorf("agent model %q does not match manifest model %q", m.Agent.Model, m.ModelID)
	}
	if m.ProjectContextMode != s2sbench.ProjectContextConfigured && m.ProjectContextMode != s2sbench.ProjectContextColdStart {
		return fmt.Errorf("project context mode %q is not supported", m.ProjectContextMode)
	}
	if strings.TrimSpace(m.Target.Engine) == "" || strings.TrimSpace(m.Target.Dialect) == "" {
		return fmt.Errorf("execution target engine and dialect are required")
	}
	wantBudget := budgetForSuite(m.Suite)
	if m.Budget != wantBudget {
		return fmt.Errorf("%s budget %+v differs from frozen %+v", m.Suite, m.Budget, wantBudget)
	}
	expected, err := ScenariosForSuite(m.Suite)
	customSuite := strings.TrimSpace(m.SuiteDigest) != ""
	if err != nil && !customSuite {
		return err
	}
	if m.Suite == SuiteFormal {
		if strings.Join(m.PriorSampleOverlap, "\x00") != strings.Join(FormalPriorSampleOverlap, "\x00") {
			return fmt.Errorf("formal prior-sample overlap declaration differs from frozen frame")
		}
	} else if len(m.PriorSampleOverlap) != 0 {
		return fmt.Errorf("%s manifest declares formal prior-sample overlap", m.Suite)
	}
	if len(m.Scenarios) == 0 {
		return fmt.Errorf("%s manifest has no scenarios", m.Suite)
	}
	if customSuite {
		expected = append([]ScenarioSpec(nil), m.Scenarios...)
	}
	byName := make(map[string]ScenarioSpec, len(expected))
	order := make(map[string]int, len(expected))
	for index, spec := range expected {
		byName[spec.Name], order[spec.Name] = spec, index
	}
	previous := -1
	for index, actual := range m.Scenarios {
		want, ok := byName[actual.Name]
		if !ok || actual != want || order[actual.Name] <= previous {
			return fmt.Errorf("suite %q scenario %d is not a frozen ordered subset", m.Suite, index)
		}
		previous = order[actual.Name]
		if customSuite {
			if actual.Query == nil || actual.ExpectedResult == nil {
				return fmt.Errorf("workload scenario %q has no query or executable oracle", actual.Name)
			}
		} else {
			scenario, ok := scenarios.ByName(actual.Name)
			if !ok || scenario.ExpectedResult == nil {
				return fmt.Errorf("smoke scenario %q has no canonical executable oracle", actual.Name)
			}
			if got := s2sbench.StratumOf(scenario); got != actual.Stratum {
				return fmt.Errorf("smoke scenario %q stratum %q, want %q", actual.Name, actual.Stratum, got)
			}
		}
	}
	if m.AnalysisSeed != AnalysisSeed || m.BootstrapResamples != 10_000 || m.EquivalenceMargin != 0.05 {
		return fmt.Errorf("analysis parameters differ from the frozen frame")
	}
	return nil
}

func budgetForSuite(suite Suite) s2sbench.TaskBudget {
	if suite == SuiteFormal {
		return FormalBudget
	}
	return FrozenBudget
}

func ResolveScenarios(manifest Manifest) ([]scenarios.Scenario, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	out := make([]scenarios.Scenario, len(manifest.Scenarios))
	for index, spec := range manifest.Scenarios {
		if spec.Query != nil && spec.ExpectedResult != nil {
			out[index] = scenarios.Scenario{
				Name: spec.Name, Query: *spec.Query, ExpectedResult: spec.ExpectedResult,
				Verification: scenarios.Verification{Compiler: scenarios.ContractRequired, Result: scenarios.ContractRequired},
			}
			continue
		}
		scenario, ok := scenarios.ByName(spec.Name)
		if !ok {
			return nil, fmt.Errorf("unknown canonical scenario %q", spec.Name)
		}
		out[index] = scenario
	}
	return out, nil
}

const sharedPromptTemplate = `You are the analytical query agent in a controlled benchmark.
Answer only the business question.
Use only the semantic interface available in the current isolated environment: its read-only workspace files or its configured semantic service.
%[3]s
Do not inspect Git metadata, repository history, branches, installed packages, binaries, source trees, system directories, or any path outside the current workspace.
Do not locate, open, or query the benchmark database. The harness executes and validates your answer.
Do not use web access, external search, unconfigured MCP servers, plugins, skills, memories, subagents, or browser state.
Return exactly one bare JSON object and no Markdown fence or prose outside it.
For the read-only workspace-file arm, when its semantics and schema are sufficient, return {"status":"ready","dialect":"%[2]s","sql":"SELECT ...","parameters":[]}.
For the configured semantic-service arm, prefer query_metrics for result questions and return {"status":"queried"} after it succeeds; the harness consumes the structured tool result directly. The deployed MCP tool set is otherwise unrestricted.
When they are ambiguous or insufficient, return {"status":"not_ready","reason":"<category>"}, where category is one of ambiguous_identity, ambiguous_relationship, unsupported_semantics, missing_expression, missing_physical_field, or insufficient_scope.
Any authored SQL must be one read-only %[1]s statement. Do not use or infer the benchmark's expected result.`

const workspaceSchemaScope = "The physical %s schema is available in the workspace."
const semanticServiceScope = "The configured semantic service owns physical lowering and execution; no physical schema file is exposed in the workspace."

var SharedPrompt = fmt.Sprintf(sharedPromptTemplate, DuckDBTarget.Engine, DuckDBTarget.Dialect, fmt.Sprintf(workspaceSchemaScope, DuckDBTarget.Engine))

func TurnPrompt(question, failure string, budget s2sbench.TaskBudget) string {
	return TurnPromptForTarget(question, failure, budget, DuckDBTarget)
}

func TurnPromptForTarget(question, failure string, budget s2sbench.TaskBudget, target Target) string {
	return turnPromptForScope(question, failure, budget, target, fmt.Sprintf(workspaceSchemaScope, target.Engine))
}

func TurnPromptForArm(question, failure string, budget s2sbench.TaskBudget, target Target, arm Arm) string {
	scope := fmt.Sprintf(workspaceSchemaScope, target.Engine)
	if arm == ArmMetisMCP {
		scope = semanticServiceScope
	}
	return turnPromptForScope(question, failure, budget, target, scope)
}

func turnPromptForScope(question, failure string, budget s2sbench.TaskBudget, target Target, scope string) string {
	prompt := fmt.Sprintf(sharedPromptTemplate, target.Engine, target.Dialect, scope) + "\n\nBusiness question:\n" + strings.TrimSpace(question)
	if strings.TrimSpace(failure) != "" {
		prompt += "\n\nYour previous answer failed with this representation-neutral validation result:\n" + strings.TrimSpace(failure) + "\nRepair only the answer."
	}
	return fmt.Sprintf("%s\n\nYou may make at most %d observable tool calls across this question. The complete question, including any repair, has a %d-second wall-clock limit.", prompt, budget.ToolCalls, budget.QuestionTimeoutMS/1000)
}
