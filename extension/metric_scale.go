package extension

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/meaningforge/metis/ossie"
)

const (
	MetricScaleVendor     ossie.Vendor = "metis.semantic"
	MetricScaleKind                    = "metric_scale"
	MetricScaleVersion                 = "1"
	MetricScaleCapability              = "semantic.metric_scale"
)

// Evidence is typed semantic evidence derived from a semantic-critical custom
// extension. Evidence is runtime-only and never becomes a second Ossie schema.
type Evidence interface {
	ExtensionIdentity() Identity
}

// MetricScaleEvidence changes the value semantics of one metric by applying a
// positive multiplicative scale after its declared metric expression.
type MetricScaleEvidence struct {
	Identity Identity
	Metric   string
	Version  string
	Factor   float64
}

func (e MetricScaleEvidence) ExtensionIdentity() Identity { return e.Identity }

func (e MetricScaleEvidence) FactorSQL() string {
	return strconv.FormatFloat(e.Factor, 'g', -1, 64)
}

type metricScalePayload struct {
	Kind    string  `json:"kind"`
	Version string  `json:"version"`
	Factor  float64 `json:"factor"`
}

// MetricScaleInterpreter is the semantic-critical
// interpreter. The vendor payload is explicit JSON and is recognized only on a
// metric owner when kind=metric_scale.
type MetricScaleInterpreter struct{}

func (MetricScaleInterpreter) Vendor() ossie.Vendor { return MetricScaleVendor }

func (MetricScaleInterpreter) Requirements(location Location, custom ossie.CustomExtension) ([]Requirement, error) {
	evidence, ok, err := metricScaleEvidence(location, custom)
	if err != nil || !ok {
		return nil, err
	}
	return []Requirement{{
		Identity:   evidence.Identity,
		Version:    evidence.Version,
		Capability: MetricScaleCapability,
		Critical:   true,
	}}, nil
}

// MetricScaleEvidenceForMetric returns typed semantic evidence for recognized
// metric-scale extensions while leaving unrelated custom-extension vendors and
// kinds opaque.
func MetricScaleEvidenceForMetric(metric *ossie.Metric) ([]MetricScaleEvidence, error) {
	if metric == nil {
		return nil, nil
	}
	location := Location{Scope: "metric", Owner: metric.Name}
	var out []MetricScaleEvidence
	for _, custom := range metric.CustomExtensions {
		if custom.VendorName != MetricScaleVendor {
			continue
		}
		evidence, ok, err := metricScaleEvidence(location, custom)
		if err != nil {
			return nil, err
		}
		if ok {
			if len(out) != 0 {
				return nil, fmt.Errorf("metric %q declares multiple %s extensions", metric.Name, MetricScaleKind)
			}
			out = append(out, evidence)
		}
	}
	return out, nil
}

func metricScaleEvidence(location Location, custom ossie.CustomExtension) (MetricScaleEvidence, bool, error) {
	if custom.VendorName != MetricScaleVendor {
		return MetricScaleEvidence{}, false, nil
	}
	var payload metricScalePayload
	if err := json.Unmarshal([]byte(custom.Data), &payload); err != nil {
		return MetricScaleEvidence{}, false, fmt.Errorf("decode %s extension: %w", MetricScaleKind, err)
	}
	if payload.Kind != MetricScaleKind {
		return MetricScaleEvidence{}, false, nil
	}
	if location.Scope != "metric" || location.Owner == "" {
		return MetricScaleEvidence{}, false, fmt.Errorf("%s extension requires metric scope", MetricScaleKind)
	}
	if payload.Version != MetricScaleVersion {
		return MetricScaleEvidence{}, false, fmt.Errorf("%s extension version %q is not interpretable; want %q", MetricScaleKind, payload.Version, MetricScaleVersion)
	}
	if math.IsNaN(payload.Factor) || math.IsInf(payload.Factor, 0) || payload.Factor <= 0 {
		return MetricScaleEvidence{}, false, fmt.Errorf("%s factor must be a finite positive number", MetricScaleKind)
	}
	identity := Identity{Namespace: string(MetricScaleVendor), Kind: MetricScaleKind, Scope: location.Scope}
	return MetricScaleEvidence{Identity: identity, Metric: location.Owner, Version: payload.Version, Factor: payload.Factor}, true, nil
}

func MetricScaleRegistration(renderer RendererContext) Registration {
	return Registration{
		Identity: Identity{Namespace: string(MetricScaleVendor), Kind: MetricScaleKind, Scope: "metric"},
		Version:  MetricScaleVersion, Capability: MetricScaleCapability, Renderer: renderer,
	}
}
