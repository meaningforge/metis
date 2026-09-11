package builder

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

// derivePopulationPreservationEvidence records whether joins preserve each
// source population required by an aggregate metric.
func derivePopulationPreservationEvidence(datasets map[string]*ossie.Dataset, root string, expressionDatasets []string, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate, joins []semanticplan.Join) ([]semanticplan.PopulationPreservationEvidence, error) {
	if len(joins) == 0 {
		return nil, nil
	}
	filtersRootOwned := true
	for _, predicate := range predicates {
		filtersRootOwned = filtersRootOwned && predicate.Dataset == root
	}
	groupsRootOwned := true
	for _, group := range groups {
		groupsRootOwned = groupsRootOwned && group.Dataset == root
	}
	expressionRootOwned := true
	for _, dataset := range expressionDatasets {
		expressionRootOwned = expressionRootOwned && dataset == root
	}
	joinedFilterMembership := joinedFilterDefinesMembership(root, predicates, joins)
	out := make([]semanticplan.PopulationPreservationEvidence, 0, len(joins))
	for _, join := range joins {
		multiplicity, _, err := relationshipJoinMultiplicity(datasets, join)
		if err != nil {
			return nil, err
		}
		relationship := ""
		if join.Relationship != nil {
			relationship = join.Relationship.Name
		}
		out = append(out, semanticplan.PopulationPreservationEvidence{
			Relationship: relationship,
			FromDataset:  join.FromDataset,
			ToDataset:    join.ToDataset,
			Multiplicity: multiplicity,
			Obligations: []semanticplan.PopulationPreservationObligationEvidence{
				populationJoinRowsEvidence(joinedFilterMembership),
				populationFilterEvidence(filtersRootOwned, joinedFilterMembership),
				populationDatasetOwnershipEvidence(semanticplan.PopulationPreservationGrouping, groupsRootOwned),
				populationDatasetOwnershipEvidence(semanticplan.PopulationPreservationExpressionReferences, expressionRootOwned),
			},
		})
	}
	return out, nil
}

func joinedFilterDefinesMembership(root string, predicates []semanticplan.Predicate, joins []semanticplan.Join) bool {
	if len(joins) != 1 {
		return false
	}
	target := joins[0].ToDataset
	foundNullRejecting := false
	for _, predicate := range predicates {
		switch predicate.Dataset {
		case root:
		case target:
			foundNullRejecting = foundNullRejecting || nullRejectingFilter(predicate.Filter.Operator)
		default:
			return false
		}
	}
	return foundNullRejecting
}

func nullRejectingFilter(operator query.FilterOperator) bool {
	switch operator {
	case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE,
		query.FilterIN, query.FilterNotIn, query.FilterBetween, query.FilterIsNotNull:
		return true
	default:
		return false
	}
}

func populationJoinRowsEvidence(joinedFilterMembership bool) semanticplan.PopulationPreservationObligationEvidence {
	if joinedFilterMembership {
		return semanticplan.PopulationPreservationObligationEvidence{Obligation: semanticplan.PopulationPreservationJoinRows, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofJoinedFilterMembership}
	}
	return semanticplan.PopulationPreservationObligationEvidence{Obligation: semanticplan.PopulationPreservationJoinRows, Status: semanticplan.PopulationPreservationUnproven, Proof: semanticplan.PopulationProofInnerJoinTotalityUnproven}
}

func populationFilterEvidence(rootOwned, joinedFilterMembership bool) semanticplan.PopulationPreservationObligationEvidence {
	if joinedFilterMembership {
		return semanticplan.PopulationPreservationObligationEvidence{Obligation: semanticplan.PopulationPreservationFilterPlacement, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofJoinedFilterMembership}
	}
	return populationDatasetOwnershipEvidence(semanticplan.PopulationPreservationFilterPlacement, rootOwned)
}

func populationDatasetOwnershipEvidence(obligation semanticplan.PopulationPreservationObligation, rootOwned bool) semanticplan.PopulationPreservationObligationEvidence {
	if rootOwned {
		return semanticplan.PopulationPreservationObligationEvidence{Obligation: obligation, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofRootDatasetOwned}
	}
	return semanticplan.PopulationPreservationObligationEvidence{Obligation: obligation, Status: semanticplan.PopulationPreservationUnproven, Proof: semanticplan.PopulationProofJoinedDatasetDependent}
}
