// Command evidence renders target-level conformance evidence from executable
// registries shared by compiler and real-engine correctness gates.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/reference"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func main() {
	content, err := render(evidence.Targets, scenarios.Core, scenarios.ExecutionCore(), reference.Baseline)
	if err != nil {
		fatalf("render target evidence: %v", err)
	}
	if _, err := os.Stdout.Write(content); err != nil {
		fatalf("write target evidence: %v", err)
	}
}

type targetResultEvidence struct {
	required              int
	unsupported           int
	intentionalDifference int
	exceptions            []targetException
}

type targetException struct {
	target   string
	scenario string
	status   reference.TargetStatus
	reason   string
}

func render(registry []evidence.Target, compilerScenarios, executionScenarios []scenarios.Scenario, referenceCases []reference.Case) ([]byte, error) {
	targets := append([]evidence.Target(nil), registry...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Dialect < targets[j].Dialect })

	compilerRequired := 0
	resultRequired := 0
	for _, scenario := range compilerScenarios {
		if scenario.Verification.Compiler == scenarios.ContractRequired {
			compilerRequired++
		}
		if scenario.Verification.Result == scenarios.ContractRequired {
			resultRequired++
		}
	}

	referenceByScenario := make(map[string]reference.Case, len(referenceCases))
	canonicalByName := make(map[string]scenarios.Scenario, len(compilerScenarios))
	for _, scenario := range compilerScenarios {
		canonicalByName[scenario.Name] = scenario
	}
	referenceRequired := 0
	referenceCategories := make(map[string]struct{})
	referenceCapabilities := make(map[string]struct{})
	for _, c := range referenceCases {
		if _, exists := referenceByScenario[c.Scenario]; exists {
			return nil, fmt.Errorf("duplicate reference scenario %q", c.Scenario)
		}
		referenceByScenario[c.Scenario] = c
		if c.Importance != reference.ImportanceRequired {
			continue
		}
		scenario, ok := canonicalByName[c.Scenario]
		if !ok {
			return nil, fmt.Errorf("required reference scenario %q is not canonical", c.Scenario)
		}
		referenceRequired++
		referenceCategories[string(scenario.Category)] = struct{}{}
		for _, capability := range scenario.Requires {
			referenceCapabilities[string(capability)] = struct{}{}
		}
	}

	resultEvidence := make(map[string]targetResultEvidence)
	allExceptions := make([]targetException, 0)
	for _, target := range targets {
		if !target.RealEngine {
			continue
		}
		coverage, err := resolveTargetResultEvidence(target, executionScenarios, referenceByScenario)
		if err != nil {
			return nil, err
		}
		resultEvidence[target.Dialect] = coverage
		allExceptions = append(allExceptions, coverage.exceptions...)
	}
	sort.Slice(allExceptions, func(i, j int) bool {
		if allExceptions[i].target != allExceptions[j].target {
			return allExceptions[i].target < allExceptions[j].target
		}
		return allExceptions[i].scenario < allExceptions[j].scenario
	})

	var report strings.Builder
	report.WriteString("# Semantic Target Conformance Evidence\n\n")
	report.WriteString("This view is rendered from the executable target, scenario, and reference-support registries. It records correctness evidence only; it does not assert production support, SLA, or deployment availability.\n\n")
	fmt.Fprintf(&report, "- Canonical compiler scenarios: **%d**\n", compilerRequired)
	fmt.Fprintf(&report, "- Canonical result scenarios: **%d**\n", resultRequired)
	fmt.Fprintf(&report, "- Shared real-engine execution scenarios: **%d**\n", len(executionScenarios))
	fmt.Fprintf(&report, "- Required reference reality scenarios: **%d**\n", referenceRequired)
	fmt.Fprintf(&report, "- Required reference categories: **%s**\n", strings.Join(sortedKeys(referenceCategories), ", "))
	fmt.Fprintf(&report, "- Required reference capabilities: **%s**\n\n", strings.Join(sortedKeys(referenceCapabilities), ", "))
	report.WriteString("| Target | Dialect | Compiler conformance | Real-engine result conformance | Production support |\n")
	report.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, target := range targets {
		compiler := "— not registered"
		if target.Compiler {
			compiler = fmt.Sprintf("✅ %d required scenarios", compilerRequired)
		}
		realEngine := "— not registered"
		if target.RealEngine {
			coverage := resultEvidence[target.Dialect]
			realEngine = fmt.Sprintf("✅ %d required; %d unsupported; %d intentional differences", coverage.required, coverage.unsupported, coverage.intentionalDifference)
		}
		fmt.Fprintf(&report, "| %s | `%s` | %s | %s | not asserted |\n", target.Name, target.Dialect, compiler, realEngine)
	}

	report.WriteString("\n## Real-engine reference exceptions\n\n")
	if len(allExceptions) == 0 {
		report.WriteString("No reference exceptions are currently declared.\n")
		return []byte(report.String()), nil
	}
	report.WriteString("| Target | Scenario | Status | Reason |\n")
	report.WriteString("| --- | --- | --- | --- |\n")
	for _, exception := range allExceptions {
		fmt.Fprintf(&report, "| `%s` | `%s` | `%s` | %s |\n", exception.target, exception.scenario, exception.status, exception.reason)
	}
	return []byte(report.String()), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func resolveTargetResultEvidence(target evidence.Target, executionScenarios []scenarios.Scenario, referenceByScenario map[string]reference.Case) (targetResultEvidence, error) {
	coverage := targetResultEvidence{}
	for _, scenario := range executionScenarios {
		if scenario.Verification.Result != scenarios.ContractRequired {
			continue
		}

		if c, ok := referenceByScenario[scenario.Name]; ok {
			expectation, err := reference.TargetExpectationFor(c, target.Dialect, target.Capabilities)
			if err != nil {
				return targetResultEvidence{}, fmt.Errorf("resolve %s on %s: %w", scenario.Name, target.Dialect, err)
			}
			switch expectation.Status {
			case reference.TargetRequired:
				coverage.required++
			case reference.TargetUnsupported:
				coverage.unsupported++
				coverage.exceptions = append(coverage.exceptions, targetException{target: target.Dialect, scenario: scenario.Name, status: expectation.Status, reason: expectation.Reason})
			case reference.TargetIntentionalDifference:
				coverage.intentionalDifference++
				coverage.exceptions = append(coverage.exceptions, targetException{target: target.Dialect, scenario: scenario.Name, status: expectation.Status, reason: expectation.Reason})
			default:
				return targetResultEvidence{}, fmt.Errorf("scenario %q resolved unknown target status %q for %s", scenario.Name, expectation.Status, target.Dialect)
			}
			continue
		}

		if missing := target.Capabilities.Missing(scenario.Requires); len(missing) != 0 {
			return targetResultEvidence{}, fmt.Errorf("non-reference execution scenario %q is not executable on %s; missing capabilities: %v", scenario.Name, target.Dialect, missing)
		}
		coverage.required++
	}
	return coverage, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "evidence: "+format+"\n", args...)
	os.Exit(1)
}
