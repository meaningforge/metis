package attribution

import "github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"

// DimensionEvidence and EvidenceBundle define the deterministic attribution
// oracle input used by focused framework tests.
type DimensionEvidence struct {
	Dimension string              `json:"dimension"`
	Result    scenarios.ResultSet `json:"result"`
}

type EvidenceBundle struct {
	Scenario   string              `json:"scenario"`
	Dimensions []DimensionEvidence `json:"dimensions"`
}
