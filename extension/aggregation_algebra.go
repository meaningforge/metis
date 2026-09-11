package extension

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/ossie"
)

// AggregationPartialState describes one interpreter-derived partial-state
// component. It is runtime evidence, not a model-authored semantic schema.
type AggregationPartialState struct {
	Name       string
	Predicated bool
}

// AggregationAlgebraEvidence is target-aware, typed evidence produced by a
// registered interpreter for an aggregation Metis does not model natively.
// Algebra values use the canonical expression vocabulary and are validated by
// Resolver before they cross the semantic planning boundary.
type AggregationAlgebraEvidence struct {
	Identity             Identity
	Metric               string
	Version              string
	Capability           string
	Function             string
	DuplicateSensitivity string
	RollupAlgebra        string
	PartialState         []AggregationPartialState
	Merge                string
	Finalize             string
}

func (e AggregationAlgebraEvidence) ExtensionIdentity() Identity { return e.Identity }

func (e AggregationAlgebraEvidence) Requirement() Requirement {
	return Requirement{
		Identity: e.Identity, Version: e.Version, Capability: e.Capability, Critical: true,
	}
}

// AggregationAlgebraInterpreter is the optional aggregation-algebra facet of an
// Interpreter. Implementations may derive algebra only for the selected target
// and unknown aggregation call site. Merely writing equivalent fields into an
// opaque custom-extension payload never invokes this contract.
type AggregationAlgebraInterpreter interface {
	Interpreter
	AggregationAlgebra(Location, ossie.CustomExtension, RendererContext, string) (AggregationAlgebraEvidence, bool, error)
}

// AggregationAlgebraForMetric asks only explicitly registered interpreters for
// evidence about one unresolved aggregation. More than one positive claim is
// ambiguous and fails closed.
func (i *Inventory) AggregationAlgebraForMetric(metric *ossie.Metric, renderer RendererContext, function string) (AggregationAlgebraEvidence, bool, error) {
	if i == nil || metric == nil || strings.TrimSpace(function) == "" {
		return AggregationAlgebraEvidence{}, false, nil
	}
	location := Location{Scope: "metric", Owner: metric.Name}
	var found AggregationAlgebraEvidence
	foundEvidence := false
	for _, custom := range metric.CustomExtensions {
		interpreter, ok := i.interpreters[custom.VendorName]
		if !ok {
			continue
		}
		algebraInterpreter, ok := interpreter.(AggregationAlgebraInterpreter)
		if !ok {
			continue
		}
		evidence, recognized, err := algebraInterpreter.AggregationAlgebra(location, custom, renderer, function)
		if err != nil {
			return AggregationAlgebraEvidence{}, false, fmt.Errorf("interpret aggregation algebra vendor=%s metric=%s function=%s: %w", custom.VendorName, metric.Name, function, err)
		}
		if !recognized {
			continue
		}
		if evidence.Identity.Namespace != string(interpreter.Vendor()) || evidence.Identity.Scope != "metric" || evidence.Metric != metric.Name {
			return AggregationAlgebraEvidence{}, false, fmt.Errorf("interpreter vendor=%s returned mismatched evidence attribution for metric %q", interpreter.Vendor(), metric.Name)
		}
		if foundEvidence {
			return AggregationAlgebraEvidence{}, false, fmt.Errorf("metric %q has multiple registered aggregation-algebra claims for function %q", metric.Name, function)
		}
		found = evidence
		foundEvidence = true
	}
	return found, foundEvidence, nil
}
