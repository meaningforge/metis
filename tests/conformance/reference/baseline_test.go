package reference

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestBaselineUsesCanonicalResultScenarios(t *testing.T) {
	seen := map[string]struct{}{}
	for _, c := range Baseline {
		if c.Scenario == "" {
			t.Fatal("reference case must name a canonical scenario")
		}
		if _, ok := seen[c.Scenario]; ok {
			t.Fatalf("duplicate reference scenario %q", c.Scenario)
		}
		seen[c.Scenario] = struct{}{}
		if c.Origin == "" {
			t.Fatalf("reference scenario %q must record an origin class", c.Scenario)
		}
		switch c.Origin {
		case OriginExternalReference, OriginProductionBug, OriginOssie, OriginManualOracle:
		default:
			t.Fatalf("reference scenario %q has unknown origin %q", c.Scenario, c.Origin)
		}
		switch c.Importance {
		case ImportanceRequired, ImportanceOptional, ImportanceExperimental:
		default:
			t.Fatalf("reference scenario %q has unknown importance %q", c.Scenario, c.Importance)
		}

		s, ok := Scenario(c)
		if !ok {
			t.Fatalf("reference scenario %q is not in the canonical corpus", c.Scenario)
		}
		if c.Importance == ImportanceRequired {
			if s.Verification.Result != scenarios.ContractRequired {
				t.Fatalf("required reference scenario %q must have a canonical result contract", c.Scenario)
			}
			if len(s.ExpectedResult.Columns) == 0 {
				t.Fatalf("required reference scenario %q must declare expected result columns", c.Scenario)
			}
			if s.ExpectedResult.Comparison != scenarios.ResultOrdered && s.ExpectedResult.Comparison != scenarios.ResultUnordered {
				t.Fatalf("required reference scenario %q must declare ordered or unordered result comparison", c.Scenario)
			}
			if len(s.ExpectedResult.Rows) == 0 {
				t.Fatalf("required reference scenario %q must not use an empty result as its oracle", c.Scenario)
			}
		}
	}
}

func TestRequiredBaselineResolvesAcrossRegisteredRealEngines(t *testing.T) {
	executionCore := make(map[string]scenarios.Scenario)
	for _, scenario := range scenarios.ExecutionCore() {
		executionCore[scenario.Name] = scenario
	}

	targets := evidence.RealEngineTargets()
	if len(targets) == 0 {
		t.Fatal("required reference coverage needs at least one registered real-engine target")
	}
	registered := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		registered[target.Dialect] = struct{}{}
	}

	for _, c := range Baseline {
		for dialect := range c.TargetOverrides {
			if _, ok := registered[dialect]; !ok {
				t.Fatalf("reference scenario %q has an override for unregistered real-engine target %s", c.Scenario, dialect)
			}
		}
		if c.Importance != ImportanceRequired {
			continue
		}
		if _, ok := executionCore[c.Scenario]; !ok {
			t.Fatalf("required reference scenario %q must be in scenarios.ExecutionCore so supported targets can execute it", c.Scenario)
		}

		executableTargets := 0
		for _, target := range targets {
			expectation, err := TargetExpectationFor(c, target.Dialect, target.Capabilities)
			if err != nil {
				t.Fatalf("resolve reference scenario %q on %s: %v", c.Scenario, target.Dialect, err)
			}
			switch expectation.Status {
			case TargetRequired:
				executableTargets++
			case TargetIntentionalDifference:
				executableTargets++
				if expectation.Reason == "" || expectation.ExpectedResult == nil {
					t.Fatalf("reference scenario %q target %s intentional difference requires reason and expected result", c.Scenario, target.Dialect)
				}
			case TargetUnsupported:
				if expectation.Reason == "" {
					t.Fatalf("reference scenario %q target %s unsupported status requires a reason", c.Scenario, target.Dialect)
				}
			default:
				t.Fatalf("reference scenario %q target %s resolved unknown status %q", c.Scenario, target.Dialect, expectation.Status)
			}
		}
		if executableTargets == 0 {
			t.Fatalf("required reference scenario %q must execute on at least one real-engine target", c.Scenario)
		}
	}
}

func TestTargetExpectationForDerivesUnsupportedFromMissingCapabilities(t *testing.T) {
	c, ok := ByScenario("simple_metric")
	if !ok {
		t.Fatal("simple_metric reference case is missing")
	}
	expectation, err := TargetExpectationFor(c, "limited-engine", scenarios.CapabilitySet{})
	if err != nil {
		t.Fatal(err)
	}
	if expectation.Status != TargetUnsupported || !strings.Contains(expectation.Reason, "missing capabilities") {
		t.Fatalf("expectation = %#v, want capability-derived unsupported status", expectation)
	}
}

func TestTargetExpectationForHonorsSparseIntentionalDifference(t *testing.T) {
	c, ok := ByScenario("simple_metric")
	if !ok {
		t.Fatal("simple_metric reference case is missing")
	}
	scenario, _ := Scenario(c)
	targetExpected := *scenario.ExpectedResult
	c.TargetOverrides = map[string]TargetExpectation{
		"CLICKHOUSE": {
			Status:         TargetIntentionalDifference,
			Reason:         "backend-specific semantic contract",
			ExpectedResult: &targetExpected,
		},
	}
	capabilities, ok := evidence.RealEngineCapabilities("CLICKHOUSE")
	if !ok {
		t.Fatal("clickhouse real-engine target is missing")
	}
	expectation, err := TargetExpectationFor(c, "CLICKHOUSE", capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if expectation.Status != TargetIntentionalDifference || expectation.Reason == "" || expectation.ExpectedResult == nil {
		t.Fatalf("expectation = %#v", expectation)
	}
}

func TestTargetExpectationForRejectsUnverifiedIntentionalDifference(t *testing.T) {
	c, ok := ByScenario("simple_metric")
	if !ok {
		t.Fatal("simple_metric reference case is missing")
	}
	c.TargetOverrides = map[string]TargetExpectation{
		"CLICKHOUSE": {Status: TargetIntentionalDifference, Reason: "backend-specific semantic contract"},
	}
	capabilities, _ := evidence.RealEngineCapabilities("CLICKHOUSE")
	_, err := TargetExpectationFor(c, "CLICKHOUSE", capabilities)
	if err == nil || !strings.Contains(err.Error(), "requires a target expected result") {
		t.Fatalf("TargetExpectationFor unverified intentional difference error = %v", err)
	}
}

func TestRequiredBaselineCoversEveryCanonicalCategory(t *testing.T) {
	baselineCategories := map[scenarios.Category]struct{}{}
	for _, c := range Baseline {
		if c.Importance != ImportanceRequired {
			continue
		}
		s, _ := Scenario(c)
		baselineCategories[s.Category] = struct{}{}
	}
	canonicalCategories := map[scenarios.Category]struct{}{}
	for _, scenario := range scenarios.Core {
		canonicalCategories[scenario.Category] = struct{}{}
	}
	for category := range canonicalCategories {
		if _, ok := baselineCategories[category]; !ok {
			t.Fatalf("required reference baseline must include category %q", category)
		}
	}
}

func TestRequiredBaselineCoversEveryRegisteredRealEngineCapability(t *testing.T) {
	requiredScenarios := make([]scenarios.Scenario, 0, len(Baseline))
	for _, c := range Baseline {
		if c.Importance != ImportanceRequired {
			continue
		}
		scenario, ok := Scenario(c)
		if !ok {
			t.Fatalf("reference scenario %q is not canonical", c.Scenario)
		}
		requiredScenarios = append(requiredScenarios, scenario)
	}

	registeredCapabilities := scenarios.CapabilitySet{}
	for _, target := range evidence.RealEngineTargets() {
		for capability := range target.Capabilities {
			registeredCapabilities[capability] = struct{}{}
		}
	}
	if len(registeredCapabilities) == 0 {
		t.Fatal("required reality coverage needs registered real-engine capabilities")
	}

	for capability := range registeredCapabilities {
		covered := false
		for _, scenario := range requiredScenarios {
			if scenarioRequires(scenario, capability) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("required reality baseline has no scenario for registered capability %q", capability)
		}
	}
}

func scenarioRequires(scenario scenarios.Scenario, capability scenarios.Capability) bool {
	for _, required := range scenario.Requires {
		if required == capability {
			return true
		}
	}
	return false
}
