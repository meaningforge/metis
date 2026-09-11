package semanticplan

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/serrors"
)

type PopulationPreservationObligation string

const (
	PopulationPreservationJoinRows             PopulationPreservationObligation = "join_rows"
	PopulationPreservationFilterPlacement      PopulationPreservationObligation = "filter_placement"
	PopulationPreservationGrouping             PopulationPreservationObligation = "grouping"
	PopulationPreservationExpressionReferences PopulationPreservationObligation = "expression_references"
)

type PopulationPreservationStatus string

const (
	PopulationPreservationProven   PopulationPreservationStatus = "PROVEN"
	PopulationPreservationUnproven PopulationPreservationStatus = "UNPROVEN"
)

type PopulationPreservationProof string

const (
	PopulationProofInnerJoinTotalityUnproven PopulationPreservationProof = "inner_join_totality_unproven"
	PopulationProofRootDatasetOwned          PopulationPreservationProof = "root_dataset_owned"
	PopulationProofJoinedDatasetDependent    PopulationPreservationProof = "joined_dataset_dependent"
	PopulationProofJoinedFilterMembership    PopulationPreservationProof = "joined_filter_defines_membership"
)

type RelationshipMultiplicity string

const (
	RelationshipAtMostOne RelationshipMultiplicity = "AT_MOST_ONE"
	RelationshipMayFanout RelationshipMultiplicity = "MAY_FAN_OUT"
)

type FanoutAdmissionEvidence struct {
	Aggregation          string                          `json:"aggregation"`
	DuplicateSensitivity expression.DuplicateSensitivity `json:"duplicate_sensitivity"`
}

type PopulationPreservationObligationEvidence struct {
	Obligation PopulationPreservationObligation `json:"obligation"`
	Status     PopulationPreservationStatus     `json:"status"`
	Proof      PopulationPreservationProof      `json:"proof"`
}

type PopulationPreservationEvidence struct {
	Relationship string                                     `json:"relationship"`
	FromDataset  string                                     `json:"from_dataset"`
	ToDataset    string                                     `json:"to_dataset"`
	Multiplicity RelationshipMultiplicity                   `json:"multiplicity"`
	Obligations  []PopulationPreservationObligationEvidence `json:"obligations"`
	Admission    *FanoutAdmissionEvidence                   `json:"fanout_admission,omitempty"`
}

func (e PopulationPreservationEvidence) Preserved() bool {
	if len(e.Obligations) != 4 {
		return false
	}
	seen := map[PopulationPreservationObligation]struct{}{}
	for _, obligation := range e.Obligations {
		if obligation.Status != PopulationPreservationProven || !validPopulationPreservationProof(obligation) {
			return false
		}
		seen[obligation.Obligation] = struct{}{}
	}
	return len(seen) == 4
}

func validPopulationPreservationProof(e PopulationPreservationObligationEvidence) bool {
	switch e.Obligation {
	case PopulationPreservationJoinRows:
		return (e.Status == PopulationPreservationUnproven && e.Proof == PopulationProofInnerJoinTotalityUnproven) ||
			(e.Status == PopulationPreservationProven && e.Proof == PopulationProofJoinedFilterMembership)
	case PopulationPreservationFilterPlacement:
		return (e.Status == PopulationPreservationProven && (e.Proof == PopulationProofRootDatasetOwned || e.Proof == PopulationProofJoinedFilterMembership)) ||
			(e.Status == PopulationPreservationUnproven && e.Proof == PopulationProofJoinedDatasetDependent)
	case PopulationPreservationGrouping, PopulationPreservationExpressionReferences:
		return (e.Status == PopulationPreservationProven && e.Proof == PopulationProofRootDatasetOwned) ||
			(e.Status == PopulationPreservationUnproven && e.Proof == PopulationProofJoinedDatasetDependent)
	default:
		return false
	}
}

// ValidatePopulationPreservationEvidence checks the closed proof carried by a
// source-aware semantic node.
func ValidatePopulationPreservationEvidence(e PopulationPreservationEvidence) error {
	if e.Relationship == "" || e.FromDataset == "" || e.ToDataset == "" {
		return serrors.Internal("population-preservation evidence is missing relationship identity", nil)
	}
	want := map[PopulationPreservationObligation]struct{}{
		PopulationPreservationJoinRows: {}, PopulationPreservationFilterPlacement: {},
		PopulationPreservationGrouping: {}, PopulationPreservationExpressionReferences: {},
	}
	seen := make(map[PopulationPreservationObligation]struct{}, len(e.Obligations))
	for _, obligation := range e.Obligations {
		if _, ok := want[obligation.Obligation]; !ok {
			return serrors.Internal("population-preservation evidence has an unknown obligation", map[string]any{"obligation": obligation.Obligation})
		}
		if _, ok := seen[obligation.Obligation]; ok {
			return serrors.Internal("population-preservation evidence repeats an obligation", map[string]any{"obligation": obligation.Obligation})
		}
		seen[obligation.Obligation] = struct{}{}
		if !validPopulationPreservationProof(obligation) {
			return serrors.Internal("population-preservation evidence has a mismatched proof", map[string]any{"obligation": obligation.Obligation, "status": obligation.Status, "proof": obligation.Proof})
		}
	}
	if len(seen) != len(want) {
		return serrors.Internal("population-preservation evidence is incomplete", map[string]any{"relationship": e.Relationship})
	}
	switch e.Multiplicity {
	case RelationshipAtMostOne:
		if e.Admission != nil {
			return serrors.Internal("non-fanout relationship carries fanout admission evidence", map[string]any{"relationship": e.Relationship})
		}
	case RelationshipMayFanout:
		if e.Admission == nil || e.Admission.Aggregation == "" || e.Admission.DuplicateSensitivity != expression.DuplicateInvariant || !e.Preserved() {
			return serrors.Internal("fanout relationship lacks complete admission evidence", map[string]any{"relationship": e.Relationship})
		}
	default:
		return serrors.Internal("population-preservation evidence has unknown relationship multiplicity", map[string]any{"relationship": e.Relationship, "multiplicity": e.Multiplicity})
	}
	return nil
}
