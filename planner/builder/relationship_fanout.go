package builder

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// admitMetricRelationshipJoin fails closed unless the join is at-most-one or
// duplicate-invariant aggregation has complete population-preservation proof.
func admitMetricRelationshipJoin(join semanticplan.Join, evidence *semanticplan.PopulationPreservationEvidence, resolved expression.ResolvedExpression) error {
	if evidence == nil {
		return serrors.Internal("relationship traversal is missing population-preservation evidence", nil)
	}
	if evidence.Multiplicity == semanticplan.RelationshipAtMostOne {
		return nil
	}
	properties := resolved.AggregationPropertyValues()
	aggregation := ""
	duplicate := expression.DuplicateUnknown
	derived := false
	if len(properties) == 1 {
		aggregation = properties[0].Function
		duplicate = properties[0].Duplicate
		derived = properties[0].Derived
	}
	if derived && duplicate == expression.DuplicateInvariant && evidence.Preserved() {
		evidence.Admission = &semanticplan.FanoutAdmissionEvidence{Aggregation: aggregation, DuplicateSensitivity: duplicate}
		return nil
	}
	failed := make([]string, 0, len(evidence.Obligations))
	for _, obligation := range evidence.Obligations {
		if obligation.Status != semanticplan.PopulationPreservationProven {
			failed = append(failed, string(obligation.Obligation))
		}
	}
	details := map[string]any{
		"relationship":          join.Relationship.Name,
		"from_dataset":          join.FromDataset,
		"to_dataset":            join.ToDataset,
		"aggregation_derived":   derived,
		"duplicate_sensitivity": duplicate,
		"population_preserved":  evidence.Preserved(),
		"unproven_obligations":  failed,
	}
	if aggregation != "" {
		details["aggregation"] = aggregation
	}
	return &serrors.Error{
		Code:    serrors.ErrUnsupportedRelationshipFanout,
		Message: "relationship fanout is not proven safe for the selected metric population",
		Details: details,
		Suggestions: []string{
			"use a duplicate-invariant aggregation such as MIN, MAX, or COUNT(DISTINCT ...)",
			"constrain the joined dataset with a null-rejecting filter while keeping grouping and metric expression references on the source dataset",
			"declare a primary or unique key on the relationship target when the target is semantically unique",
		},
	}
}

// validateNonFanoutJoin proves that the dataset being joined into the current
// traversal contributes at most one row for each source row. Temporal joins
// have their own explicit point-in-time cardinality contract because an SCD
// target intentionally repeats business keys across validity versions.
// validateNonFanoutJoin requires relationship target uniqueness for an
// ordinary join; temporal joins use their explicit cardinality contract.
func validateNonFanoutJoin(datasets map[string]*ossie.Dataset, join semanticplan.Join) error {
	multiplicity, targetColumns, err := relationshipJoinMultiplicity(datasets, join)
	if err != nil {
		return err
	}
	if multiplicity == semanticplan.RelationshipAtMostOne {
		return nil
	}
	return &serrors.Error{Code: serrors.ErrUnsupportedRelationshipFanout, Message: "relationship traversal may fan out because target join columns are not proven unique", Details: map[string]any{
		"relationship": join.Relationship.Name, "from_dataset": join.FromDataset, "to_dataset": join.ToDataset, "target_columns": append([]string(nil), targetColumns...),
	}}
}

func relationshipJoinMultiplicity(datasets map[string]*ossie.Dataset, join semanticplan.Join) (semanticplan.RelationshipMultiplicity, []string, error) {
	if join.Relationship == nil {
		return "", nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "join relationship is required"}
	}
	if join.Temporal != nil {
		return semanticplan.RelationshipAtMostOne, nil, nil
	}
	rel := join.Relationship
	target := datasets[join.ToDataset]
	if target == nil {
		return "", nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "join target dataset is not present in semantic model", Details: map[string]any{"dataset": join.ToDataset}}
	}
	var targetColumns []string
	switch {
	case join.FromDataset == rel.From && join.ToDataset == rel.To:
		targetColumns = rel.ToColumns
	case join.FromDataset == rel.To && join.ToDataset == rel.From:
		targetColumns = rel.FromColumns
	default:
		return "", nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "planned join does not match relationship endpoints", Details: map[string]any{"relationship": rel.Name}}
	}
	if datasetColumnsAreUnique(target, targetColumns) {
		return semanticplan.RelationshipAtMostOne, targetColumns, nil
	}
	return semanticplan.RelationshipMayFanout, targetColumns, nil
}

func datasetColumnsAreUnique(dataset *ossie.Dataset, columns []string) bool {
	if dataset == nil || len(columns) == 0 {
		return false
	}
	if sameColumnSet(dataset.PrimaryKey, columns) {
		return true
	}
	for _, key := range dataset.UniqueKeys {
		if sameColumnSet(key, columns) {
			return true
		}
	}
	return false
}

func sameColumnSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
