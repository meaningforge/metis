package conversion

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

func customCalendarGrainToDateSQLPlan(node semanticplan.SemanticPlanNode, cteNames map[string]string, blockByAlias map[string]sqlplan.QueryBlockID, next int, expressionDialect string) ([]sqlplan.QueryBlock, error) {
	cumulative, ok := node.(semanticplan.CumulativeWindowNode)
	nodeBase := node.NodeBase()
	if !ok || cumulative.CustomCalendarGrainToDate == nil || len(nodeBase.Inputs) != 1 {
		return nil, metricLoweringError("custom grain-to-date metric requires one typed base metric", nodeBase.ID)
	}
	custom := cumulative.CustomCalendarGrainToDate
	baseAlias, ok := cteNames[nodeBase.Inputs[0].NodeID]
	if !ok {
		return nil, metricLoweringError("custom grain-to-date base metric has no evaluation node", nodeBase.ID)
	}
	baseID, ok := blockByAlias[baseAlias]
	if !ok {
		return nil, metricLoweringError("custom grain-to-date base metric has no SQLPlan block", nodeBase.ID)
	}
	var timeGroup *semanticplan.GroupBy
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == cumulative.Spec.TimeDimension {
			timeGroup = group
			break
		}
	}
	if timeGroup == nil {
		return nil, queryGrainLoweringError("custom grain-to-date time dimension must be present in query grain", nodeBase.ID)
	}
	const mappingAlias = "__metis_custom_gtd_periods"
	const queryBucket = "__metis_query_bucket"
	const resetBucket = "__metis_reset_bucket"
	const queryOrdinal = "__metis_query_ordinal"
	queryExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: timeGroup.CustomCalendar.BucketExpression, Dialect: expressionDialect})
	resetExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: custom.ResetBucketExpression, Dialect: expressionDialect})
	ordinalExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: custom.QueryOrdinalExpression, Dialect: expressionDialect})
	mapping := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next), From: sourceRelation(custom.Dataset), Projections: []sqlplan.Projection{{Expr: queryExpr, Alias: queryBucket}, {Expr: resetExpr, Alias: resetBucket}, {Expr: ordinalExpr, Alias: queryOrdinal}}, GroupBy: []sqlplan.Expr{queryExpr, resetExpr, ordinalExpr}}
	block := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + 1), Inputs: []sqlplan.QueryInput{{Alias: baseAlias, Block: baseID, Mode: sqlplan.QueryInputCTE}, {Alias: mappingAlias, Block: mapping.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseAlias}, Alias: baseAlias}}
	block.Joins = append(block.Joins, sqlplan.Join{Kind: sqlplan.JoinInner, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: mappingAlias}, Alias: mappingAlias}, On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: baseAlias, Name: timeGroup.Name}, Operator: "=", Right: sqlplan.ColumnRef{Table: mappingAlias, Name: queryBucket}}})
	partition := make([]sqlplan.Expr, 0, len(nodeBase.OutputGrain))
	for _, group := range nodeBase.OutputGrain {
		column := sqlplan.ColumnRef{Table: baseAlias, Name: group.Name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
		if group.Name != timeGroup.Name {
			partition = append(partition, column)
		}
	}
	partition = append(partition, sqlplan.ColumnRef{Table: mappingAlias, Name: resetBucket})
	window := sqlplan.WindowExpr{Function: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: baseAlias, Name: cumulative.Spec.BaseMetric}}}, PartitionBy: partition, OrderBy: []sqlplan.Expr{sqlplan.ColumnRef{Table: mappingAlias, Name: queryOrdinal}}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: window, Alias: nodeBase.ID})
	return []sqlplan.QueryBlock{mapping, block}, nil
}
