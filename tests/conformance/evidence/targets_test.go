package evidence_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestTargetRegistryIsUnambiguous(t *testing.T) {
	seenNames := map[string]struct{}{}
	seenDialects := map[string]struct{}{}
	if len(evidence.Targets) == 0 {
		t.Fatal("conformance target registry is empty")
	}
	for _, target := range evidence.Targets {
		if target.Name == "" || target.Dialect == "" {
			t.Fatalf("target must have name and dialect: %#v", target)
		}
		if !target.Compiler && !target.RealEngine {
			t.Fatalf("target %q has no executable evidence layer", target.Name)
		}
		if target.RealEngine && !target.Compiler {
			t.Fatalf("real-engine target %q must also have compiler evidence", target.Name)
		}
		if target.RealEngine && len(target.Capabilities) == 0 {
			t.Fatalf("real-engine target %q must declare semantic capabilities", target.Name)
		}
		if !target.RealEngine && len(target.Capabilities) != 0 {
			t.Fatalf("non-real-engine target %q must not claim execution capabilities", target.Name)
		}
		if _, exists := seenNames[target.Name]; exists {
			t.Fatalf("duplicate target name %q", target.Name)
		}
		if _, exists := seenDialects[target.Dialect]; exists {
			t.Fatalf("duplicate target dialect %q", target.Dialect)
		}
		seenNames[target.Name] = struct{}{}
		seenDialects[target.Dialect] = struct{}{}
	}
}

// The compiler-evidence table is hand maintained; the physical dialect registry
// is what actually renders SQL. Everything that reads the evidence table --
// conformance accounting, the published target matrix, and the renderer
// byte-identity gate -- describes its coverage in terms of registered
// renderers, so the two sets drifting apart weakens those claims silently.
//
// A registered renderer with no evidence row is a renderer nothing measures. An
// evidence row with no registered renderer claims conformance for something
// that cannot render at all.
func TestCompilerEvidenceCoversExactlyTheRegisteredRenderers(t *testing.T) {
	evidenced := map[string]bool{}
	for _, target := range evidence.CompilerTargets() {
		evidenced[strings.ToUpper(target.Dialect)] = true
	}
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, dialect := range registry.Dialects() {
		registered[string(dialect)] = true
	}
	for dialect := range registered {
		if !evidenced[dialect] {
			t.Errorf("renderer %q is registered but has no compiler-evidence target, so nothing measures it", dialect)
		}
	}
	for dialect := range evidenced {
		if !registered[dialect] {
			t.Errorf("compiler-evidence target %q has no registered renderer", dialect)
		}
	}
}

func TestRealEngineTargetsCoverExecutionCoreCapabilities(t *testing.T) {
	for _, target := range evidence.RealEngineTargets() {
		for _, scenario := range scenarios.ExecutionCore() {
			if missing := target.Capabilities.Missing(scenario.Requires); len(missing) != 0 {
				t.Fatalf("real-engine target %q cannot execute canonical scenario %q; missing %v", target.Dialect, scenario.Name, missing)
			}
		}
	}
}

func TestRealEngineCapabilitiesAreReturnedAsOwnedCopies(t *testing.T) {
	capabilities, ok := evidence.RealEngineCapabilities("doris")
	if !ok {
		t.Fatal("doris real-engine evidence target is missing")
	}
	delete(capabilities, scenarios.CapabilityAggregation)

	fresh, ok := evidence.RealEngineCapabilities("doris")
	if !ok {
		t.Fatal("doris real-engine evidence target disappeared")
	}
	if _, ok := fresh[scenarios.CapabilityAggregation]; !ok {
		t.Fatal("real-engine capability registry leaked caller mutation")
	}
}
