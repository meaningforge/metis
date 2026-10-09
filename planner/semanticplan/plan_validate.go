package semanticplan

import (
	"fmt"
	"github.com/meaningforge/metis/query"
)

// ValidateSemanticPlan verifies structural invariants that SQLPlan construction and
// semantic-plan optimization rely on. It validates planner-owned typed state;
// individual optimization rules remain responsible for proving their own
// rewrite preconditions.
func ValidateSemanticPlan(plan *SemanticPlan) error {
	if plan == nil {
		return fmt.Errorf("semantic plan is required")
	}
	if err := validateRelationPolicies(plan); err != nil {
		return err
	}
	if plan.Root.Name == "" {
		return fmt.Errorf("semantic plan root dataset is required")
	}

	reachable, err := ValidatePlanJoinTree("plan", plan.Root.Name, plan.Joins)
	if err != nil {
		return err
	}
	if !RequiresComposedPlan(plan) {
		if err := ValidatePlanConsumerReachability(plan, reachable); err != nil {
			return err
		}
	}
	if err := validatePlanExpressions(plan); err != nil {
		return err
	}

	// The DAG contract, applied to the DAG the plan owns.
	//
	// It used to run only when the construction wrapper was present, which made
	// it a check on 59 of the 96 conformance scenarios and on none of the other
	// 37: canonical set ordering, boundary/kind agreement, duplicate inputs and
	// per-node dataset reachability went unproven for exactly the plans that
	// carry their nodes directly. Reading plan ownership makes it one rule for
	// every plan.
	if len(plan.Nodes) != 0 {
		if err := ValidateDAGContract(plan); err != nil {
			return fmt.Errorf("semantic graph: %w", err)
		}
	}

	if err := ValidateOwnedDAG(plan); err != nil {
		return err
	}
	return RequireOutputContractGrainMatchesPlan(plan)
}

func validateNodeDatasetReachability(nodes []SemanticPlanNode) error {
	for _, node := range nodes {
		source, ok := NodeSourceState(node)
		if !ok || source.Root.Name == "" {
			continue
		}
		base := node.NodeBase()
		reachable, err := ValidatePlanJoinTree(fmt.Sprintf("semantic node %q", base.ID), source.Root.Name, source.Joins)
		if err != nil {
			return err
		}
		for _, dataset := range source.RequiredDatasets {
			if dataset == "" {
				continue
			}
			if _, ok := reachable[dataset]; !ok {
				return fmt.Errorf("semantic node %q requires unreachable dataset %q", base.ID, dataset)
			}
		}
	}
	return nil
}

func validatePlanExpressions(plan *SemanticPlan) error {
	for _, projection := range plan.Projections {
		if projection.Name == "" {
			return fmt.Errorf("projection name is required")
		}
		switch projection.Kind {
		case ProjectionMetric, ProjectionDimension:
		default:
			return fmt.Errorf("projection %q has unsupported kind %q", projection.Name, projection.Kind)
		}
		if !projection.Expression.IsResolved() {
			return fmt.Errorf("projection %q has unresolved expression", projection.Name)
		}
	}
	if err := validatePredicates("plan", plan.Predicates, nil); err != nil {
		return err
	}
	for i, predicate := range plan.BooleanPredicates {
		if err := validateBooleanPredicate(predicate, nil); err != nil {
			return fmt.Errorf("plan boolean predicate %d: %w", i, err)
		}
	}
	if len(plan.RelationshipExistence) > 1 {
		return fmt.Errorf("semantic plan supports one relationship existence predicate")
	}
	for i, predicate := range plan.RelationshipExistence {
		if err := validateRelationshipExistence(plan, predicate); err != nil {
			return fmt.Errorf("plan relationship existence predicate %d: %w", i, err)
		}
	}
	if err := validateGroups("plan", plan.Groups); err != nil {
		return err
	}
	for _, sort := range plan.Sorts {
		if sort.Name == "" {
			return fmt.Errorf("sort name is required")
		}
		switch sort.Kind {
		case SortMetric, SortDimension:
		default:
			return fmt.Errorf("sort %q has unsupported kind %q", sort.Name, sort.Kind)
		}
		if !sort.Expression.IsResolved() {
			return fmt.Errorf("sort %q has unresolved expression", sort.Name)
		}
	}
	return nil
}

func validateRelationshipExistence(plan *SemanticPlan, predicate RelationshipExistencePredicate) error {
	if predicate.Relationship == nil || predicate.Relationship.Name == "" {
		return fmt.Errorf("relationship identity is required")
	}
	if predicate.Source.Name == "" || predicate.Source.Source == "" || predicate.Source.Name != plan.Root.Name {
		return fmt.Errorf("relationship existence source must match plan root")
	}
	if predicate.Target.Name == "" || predicate.Target.Source == "" || predicate.Target.Name == predicate.Source.Name {
		return fmt.Errorf("relationship existence target is invalid")
	}
	if len(predicate.Correlations) == 0 {
		return fmt.Errorf("relationship existence requires correlation keys")
	}
	for _, correlation := range predicate.Correlations {
		if correlation.SourceColumn == "" || correlation.TargetColumn == "" {
			return fmt.Errorf("relationship existence correlation is incomplete")
		}
	}
	if err := validateBooleanPredicate(predicate.Predicate, map[string]struct{}{predicate.Target.Name: {}}); err != nil {
		return fmt.Errorf("target predicate: %w", err)
	}
	relationship := predicate.Relationship
	forward := relationship.From == predicate.Source.Name && relationship.To == predicate.Target.Name
	reverse := relationship.To == predicate.Source.Name && relationship.From == predicate.Target.Name
	if !forward && !reverse {
		return fmt.Errorf("relationship endpoints do not match existence direction")
	}
	sourceColumns, targetColumns := relationship.FromColumns, relationship.ToColumns
	if reverse {
		sourceColumns, targetColumns = relationship.ToColumns, relationship.FromColumns
	}
	if len(sourceColumns) != len(predicate.Correlations) || len(targetColumns) != len(predicate.Correlations) {
		return fmt.Errorf("relationship existence correlation count does not match relationship")
	}
	for i, correlation := range predicate.Correlations {
		if correlation.SourceColumn != sourceColumns[i] || correlation.TargetColumn != targetColumns[i] {
			return fmt.Errorf("relationship existence correlation does not match relationship")
		}
	}
	return nil
}

func ValidatePlanJoinTree(scope, root string, joins []Join) (map[string]struct{}, error) {
	if root == "" {
		return nil, fmt.Errorf("%s root dataset is required", scope)
	}
	reachable := map[string]struct{}{root: {}}
	for _, join := range joins {
		if join.FromDataset == "" || join.ToDataset == "" {
			return nil, fmt.Errorf("%s contains join with empty dataset endpoint", scope)
		}
		if _, ok := reachable[join.FromDataset]; !ok {
			return nil, fmt.Errorf("%s join from %q is not reachable from root %q", scope, join.FromDataset, root)
		}
		reachable[join.ToDataset] = struct{}{}
	}
	return reachable, nil
}

func validatePredicates(scope string, predicates []Predicate, reachable map[string]struct{}) error {
	for _, predicate := range predicates {
		if predicate.Dataset == "" {
			return fmt.Errorf("%s contains predicate without dataset", scope)
		}
		if !predicate.Expression.IsResolved() {
			return fmt.Errorf("%s predicate on dataset %q has unresolved expression", scope, predicate.Dataset)
		}
		if reachable != nil {
			if _, ok := reachable[predicate.Dataset]; !ok {
				return fmt.Errorf("%s predicate dataset %q is unreachable", scope, predicate.Dataset)
			}
		}
	}
	return nil
}

func validateGroups(scope string, groups []GroupBy) error {
	for _, group := range groups {
		if group.Name == "" || group.Dataset == "" {
			return fmt.Errorf("%s contains group with empty name or dataset", scope)
		}
		if !group.Expression.IsResolved() {
			return fmt.Errorf("%s group %q has unresolved expression", scope, group.Name)
		}
	}
	return nil
}

func ValidatePlanConsumerReachability(plan *SemanticPlan, reachable map[string]struct{}) error {
	require := func(kind, name, dataset string) error {
		if dataset == "" {
			return nil
		}
		if _, ok := reachable[dataset]; !ok {
			return fmt.Errorf("%s %q requires unreachable dataset %q", kind, name, dataset)
		}
		return nil
	}
	for _, projection := range plan.Projections {
		if err := require("projection", projection.Name, projection.Dataset); err != nil {
			return err
		}
		for _, dataset := range projection.Datasets {
			if err := require("projection", projection.Name, dataset); err != nil {
				return err
			}
		}
	}
	for _, predicate := range plan.Predicates {
		if err := require("predicate", predicate.Filter.Field, predicate.Dataset); err != nil {
			return err
		}
	}
	for _, predicate := range plan.BooleanPredicates {
		if err := validateBooleanPredicate(predicate, reachable); err != nil {
			return err
		}
	}
	for _, group := range plan.Groups {
		if err := require("group", group.Name, group.Dataset); err != nil {
			return err
		}
	}
	for _, sort := range plan.Sorts {
		if err := require("sort", sort.Name, sort.Dataset); err != nil {
			return err
		}
	}
	return nil
}

func validateBooleanPredicate(predicate BooleanPredicate, reachable map[string]struct{}) error {
	if predicate.Leaf != nil {
		if predicate.Kind != "" || len(predicate.Children) != 0 {
			return fmt.Errorf("boolean leaf has logical payload")
		}
		return validatePredicates("boolean", []Predicate{*predicate.Leaf}, reachable)
	}
	if predicate.Kind == query.PredicateNot {
		if len(predicate.Children) != 1 {
			return fmt.Errorf("not predicate requires one child")
		}
	} else if predicate.Kind == query.PredicateAnd || predicate.Kind == query.PredicateOr {
		if len(predicate.Children) < 2 || len(predicate.Children) > 32 {
			return fmt.Errorf("and/or predicate requires 2 to 32 children")
		}
	} else {
		return fmt.Errorf("unsupported boolean predicate kind %q", predicate.Kind)
	}
	for _, child := range predicate.Children {
		if err := validateBooleanPredicate(child, reachable); err != nil {
			return err
		}
	}
	return nil
}
