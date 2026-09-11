package conversion

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

// ApplyPostEvaluationPredicatesToSQLPlan is the typed SQLPlan replacement for
// ApplyPostEvaluationPredicates. It runs while semanticplan.SemanticPlan is explicitly in
// scope during BuildSQLPlan and leaves renderers with no semantic work.
func ApplyPostEvaluationPredicatesToSQLPlan(physical *sqlplan.Plan, semantic *semanticplan.SemanticPlan) error {
	if physical == nil || semantic == nil {
		return nil
	}
	predicates := postEvaluationPredicates(semantic)
	if len(predicates) == 0 {
		return nil
	}
	root := sqlPlanBlock(physical, physical.Root)
	if root == nil {
		return serrors.Internal("post-evaluation predicates require an existing SQLPlan root block", nil)
	}
	metricRelations := semanticMetricRelations(semantic)
	outputRelations := []string{root.From.Alias}
	for _, join := range root.Joins {
		outputRelations = append(outputRelations, join.Relation.Alias)
	}
	for _, predicate := range predicates {
		values, err := predicateValues(predicate.Filter.Operator, predicate.Filter.Value)
		if err != nil {
			return &serrors.Error{Code: serrors.ErrInvalidFilterValue, Message: "invalid post-evaluation predicate value", Details: map[string]any{"field": predicate.Name, "cause": err.Error()}}
		}
		left := coalescedSQLPlanColumn(outputRelations, predicate.Name)
		if relation, ok := metricRelations[predicate.Name]; ok {
			left = sqlplan.ColumnRef{Table: relation, Name: predicate.Name}
		}
		root.Predicates = append(root.Predicates, sqlplan.Predicate{Left: left, Operator: predicate.Filter.Operator, Values: values})
	}
	if err := sqlplan.Validate(physical); err != nil {
		return serrors.Internal("post-evaluation predicate rewrite produced an invalid SQLPlan", map[string]any{"cause": err.Error()})
	}
	return nil
}

func sqlPlanBlock(plan *sqlplan.Plan, id sqlplan.QueryBlockID) *sqlplan.QueryBlock {
	if plan == nil {
		return nil
	}
	for i := range plan.Blocks {
		if plan.Blocks[i].ID == id {
			return &plan.Blocks[i]
		}
	}
	return nil
}

func coalescedSQLPlanColumn(relations []string, name string) sqlplan.Expr {
	if len(relations) == 1 {
		return sqlplan.ColumnRef{Table: relations[0], Name: name}
	}
	args := make([]sqlplan.Expr, 0, len(relations))
	for _, relation := range relations {
		args = append(args, sqlplan.ColumnRef{Table: relation, Name: name})
	}
	return sqlplan.FunctionCallExpr{Name: "COALESCE", Args: args}
}
