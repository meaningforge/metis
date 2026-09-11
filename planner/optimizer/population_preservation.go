package optimizer

import "github.com/meaningforge/metis/planner/semanticplan"

// filterPopulationPreservationEvidence retains proof only for surviving joins.
func filterPopulationPreservationEvidence(evidence []semanticplan.PopulationPreservationEvidence, joins []semanticplan.Join) []semanticplan.PopulationPreservationEvidence {
	wanted := make(map[string]int, len(joins))
	for _, join := range joins {
		wanted[populationPreservationJoinKey(join)]++
	}
	out := make([]semanticplan.PopulationPreservationEvidence, 0, len(evidence))
	for _, candidate := range evidence {
		key := candidate.Relationship + "\x00" + candidate.FromDataset + "\x00" + candidate.ToDataset
		if wanted[key] == 0 {
			continue
		}
		wanted[key]--
		candidate.Obligations = append([]semanticplan.PopulationPreservationObligationEvidence(nil), candidate.Obligations...)
		if candidate.Admission != nil {
			admission := *candidate.Admission
			candidate.Admission = &admission
		}
		out = append(out, candidate)
	}
	return out
}

func populationPreservationJoinKey(join semanticplan.Join) string {
	relationship := ""
	if join.Relationship != nil {
		relationship = join.Relationship.Name
	}
	return relationship + "\x00" + join.FromDataset + "\x00" + join.ToDataset
}
