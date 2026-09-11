package readiness

import (
	"reflect"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/query"
)

func TestWorkloadManifestCarriesIndependentQueryAndOracle(t *testing.T) {
	q := query.SemanticQuery{Project: "p", Model: "m", Metrics: []query.MetricRef{{Name: "revenue"}}}
	oracle := scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "revenue", ValueKind: scenarios.ResultNumber}}}, Comparison: scenarios.ResultUnordered}
	specs := []ScenarioSpec{{Name: "generated", Stratum: s2sbench.StratumControl, Question: "What is revenue?", Query: &q, ExpectedResult: &oracle}}
	agent := s2sbench.AgentIdentity{Name: "test", Version: "v1", Model: "model"}
	manifest, err := NewWorkloadManifest("generated-suite", "sha256:bundle", specs, ArmOKF, nil, agent, "provider", "model", "version", DuckDBTarget)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveScenarios(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Query.Model != "m" || resolved[0].ExpectedResult == nil {
		t.Fatalf("resolved workload scenario = %#v", resolved)
	}
}

func TestSmokeManifestFreezesTwoScenarioFrame(t *testing.T) {
	identity := testIdentity()
	manifest, err := NewSmokeManifest(ArmOKF, identity, "test-provider", "model", "test-model-revision")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.Scenarios, SmokeScenarios) {
		t.Fatalf("scenarios = %#v, want %#v", manifest.Scenarios, SmokeScenarios)
	}
	if len(manifest.Scenarios) != 2 || manifest.Scenarios[0].Stratum != s2sbench.StratumControl || manifest.Scenarios[1].Stratum != s2sbench.StratumSilentSemantics {
		t.Fatalf("smoke strata = %#v", manifest.Scenarios)
	}
}

func testIdentity() s2sbench.AgentIdentity {
	return s2sbench.AgentIdentity{Name: "test-agent", Version: "v1", Model: "model"}
}

func TestSharedPromptDoesNotNameEitherRepresentation(t *testing.T) {
	prompt := strings.ToLower(TurnPrompt("A business question?", "", FrozenBudget))
	for _, leaked := range []string{"okf", "ossie", "metis", "knowledge/index.md", "models/"} {
		if strings.Contains(prompt, leaked) {
			t.Fatalf("shared prompt leaks treatment %q", leaked)
		}
	}
}

func TestSharedPromptUsesTheSameBareJSONContractForRepair(t *testing.T) {
	for name, failure := range map[string]string{
		"first attempt": "",
		"repair":        "The read-only SQL did not parse or execute successfully.",
	} {
		t.Run(name, func(t *testing.T) {
			prompt := TurnPrompt("A business question?", failure, FrozenBudget)
			if !strings.Contains(prompt, "exactly one bare JSON object") || !strings.Contains(prompt, "no Markdown fence") {
				t.Fatalf("prompt does not state the bare JSON contract:\n%s", prompt)
			}
			if strings.Contains(prompt, "fenced JSON") {
				t.Fatalf("prompt retains the v1 fenced-JSON contract:\n%s", prompt)
			}
		})
	}
}

func TestMetisPromptDoesNotExposePhysicalSchemaFile(t *testing.T) {
	prompt := TurnPromptForArm("question", "", FrozenBudget, DuckDBTarget, ArmMetisMCP)
	if strings.Contains(prompt, "schema is available in the workspace") || !strings.Contains(prompt, "no physical schema file is exposed") || !strings.Contains(prompt, "prefer query_metrics") || !strings.Contains(prompt, "tool set is otherwise unrestricted") {
		t.Fatalf("Metis prompt physical scope:\n%s", prompt)
	}
}

func TestDiagnosticManifestFreezesOneScenarioPerStratum(t *testing.T) {
	manifest, err := NewDiagnosticManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Suite != SuiteDiagnostic || !reflect.DeepEqual(manifest.Scenarios, DiagnosticScenarios) {
		t.Fatalf("diagnostic manifest = %#v", manifest)
	}
	seen := map[s2sbench.Stratum]bool{}
	for _, scenario := range manifest.Scenarios {
		seen[scenario.Stratum] = true
	}
	for _, stratum := range s2sbench.Strata {
		if !seen[stratum] {
			t.Errorf("diagnostic frame misses stratum %q", stratum)
		}
	}
}

func TestFormalManifestUsesFiveIndependentRepetitions(t *testing.T) {
	manifest, err := NewFormalPositiveManifest(ArmMetisMCP, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Budget.Repetitions != 5 || manifest.Budget != FormalBudget {
		t.Fatalf("formal budget = %#v", manifest.Budget)
	}
	smoke, err := NewSmokeManifest(ArmMetisMCP, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if smoke.Budget.Repetitions != 1 {
		t.Fatalf("smoke repetitions = %d", smoke.Budget.Repetitions)
	}
}

func TestFormalPositiveManifestFreezesAuditedTwentyNineScenarioFrame(t *testing.T) {
	manifest, err := NewFormalPositiveManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Suite != SuiteFormal || len(manifest.Scenarios) != 29 {
		t.Fatalf("formal manifest suite=%q scenarios=%d", manifest.Suite, len(manifest.Scenarios))
	}
	wantCounts := map[s2sbench.Stratum]int{
		s2sbench.StratumSilentSemantics: 10,
		s2sbench.StratumFanout:          8,
		s2sbench.StratumEdge:            5,
		s2sbench.StratumControl:         6,
	}
	prior, err := s2sbench.SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	priorNames := map[string]bool{}
	for _, scenario := range prior {
		priorNames[scenario.Name] = true
	}
	seen := map[string]bool{}
	var actualOverlap []string
	for _, spec := range manifest.Scenarios {
		if seen[spec.Name] {
			t.Fatalf("formal scenario %q is duplicated", spec.Name)
		}
		seen[spec.Name] = true
		wantCounts[spec.Stratum]--
		if strings.TrimSpace(spec.Question) == "" {
			t.Errorf("formal scenario %q has no frozen question", spec.Name)
		}
		scenario, ok := scenarios.ByName(spec.Name)
		if !ok || scenario.ExpectedResult == nil {
			t.Errorf("formal scenario %q has no canonical executable oracle", spec.Name)
		} else if got := s2sbench.StratumOf(scenario); got != spec.Stratum {
			t.Errorf("formal scenario %q stratum=%q, want %q", spec.Name, spec.Stratum, got)
		}
		if priorNames[spec.Name] {
			actualOverlap = append(actualOverlap, spec.Name)
		}
	}
	for stratum, remaining := range wantCounts {
		if remaining != 0 {
			t.Errorf("formal stratum %q differs from frozen allocation by %d", stratum, remaining)
		}
	}
	if !reflect.DeepEqual(manifest.PriorSampleOverlap, FormalPriorSampleOverlap) {
		t.Fatalf("formal overlap = %v, want %v", manifest.PriorSampleOverlap, FormalPriorSampleOverlap)
	}
	if !reflect.DeepEqual(actualOverlap, FormalPriorSampleOverlap) {
		t.Fatalf("actual prior-sample overlap = %v, want %v", actualOverlap, FormalPriorSampleOverlap)
	}
}

func TestFormalPositiveManifestRejectsOverlapDrift(t *testing.T) {
	manifest, err := NewFormalPositiveManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	manifest.PriorSampleOverlap = nil
	if err := manifest.Validate(); err == nil {
		t.Fatal("formal manifest accepted missing prior-sample overlap declaration")
	}
}

func TestCumulativeCaseManifestFreezesOneDiagnosticPair(t *testing.T) {
	manifest, err := NewCumulativeCaseManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Suite != SuiteCumulativeCase || !reflect.DeepEqual(manifest.Scenarios, CumulativeCaseScenarios) {
		t.Fatalf("case manifest = %#v", manifest)
	}
}

func TestFiltersCaseManifestFreezesOneDiagnosticPair(t *testing.T) {
	manifest, err := NewFiltersCaseManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Suite != SuiteFiltersCase || !reflect.DeepEqual(manifest.Scenarios, FiltersCaseScenarios) {
		t.Fatalf("case manifest = %#v", manifest)
	}
}

func TestManifestRequiresExactlyOneSupportedArm(t *testing.T) {
	manifest, err := NewSmokeManifest(ArmOKF, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Arm = ""
	if err := manifest.Validate(); err == nil {
		t.Fatal("manifest accepted an empty arm")
	}
	if _, err := ParseArm("raw-ossie"); err == nil {
		t.Fatal("retired raw-ossie arm remains accepted")
	}
}

func TestManifestSelectsOrderedScenarioSubset(t *testing.T) {
	manifest, err := NewManifest(SuiteDiagnostic, ArmMetisMCP, []string{"cumulative_metric_by_month", "aggregation_variants"}, testIdentity(), "provider", "model", "revision")
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{manifest.Scenarios[0].Name, manifest.Scenarios[1].Name}; !reflect.DeepEqual(got, []string{"aggregation_variants", "cumulative_metric_by_month"}) {
		t.Fatalf("ordered subset = %v", got)
	}
	if _, err := NewManifest(SuiteSmoke, ArmOKF, []string{"not-in-suite"}, testIdentity(), "provider", "model", "revision"); err == nil {
		t.Fatal("manifest accepted a scenario outside its suite")
	}
}

func TestManifestAndPromptCarryRegisteredPhysicalTarget(t *testing.T) {
	target := Target{Engine: "snowflake", Dialect: "SNOWFLAKE"}
	manifest, err := NewManifestForTarget(SuiteSmoke, ArmOKF, nil, testIdentity(), "provider", "model", "revision", target)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Target != target {
		t.Fatalf("target = %#v, want %#v", manifest.Target, target)
	}
	prompt := TurnPromptForTarget("question", "", manifest.Budget, manifest.Target)
	if !strings.Contains(prompt, `"dialect":"SNOWFLAKE"`) || !strings.Contains(prompt, "physical snowflake schema") {
		t.Fatalf("target-aware prompt:\n%s", prompt)
	}
}
