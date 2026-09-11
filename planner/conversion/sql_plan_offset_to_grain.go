package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

func customCalendarTimeOffsetSQLPlan(node semanticplan.SemanticPlanNode, cteNames map[string]string, blockByAlias map[string]sqlplan.QueryBlockID, next int, expressionDialect string) ([]sqlplan.QueryBlock, error) {
	timeOffset, ok := node.(semanticplan.TimeOffsetNode)
	nodeBase := node.NodeBase()
	if !ok || timeOffset.CustomCalendar == nil || len(nodeBase.Inputs) != 1 || timeOffset.CustomCalendar.Count >= 0 {
		return nil, metricLoweringError("custom-calendar time offset plan is incomplete", nodeBase.ID)
	}
	custom := timeOffset.CustomCalendar
	baseAlias, ok := cteNames[nodeBase.Inputs[0].NodeID]
	if !ok {
		return nil, metricLoweringError("custom-calendar time offset base metric has no evaluation node", nodeBase.ID)
	}
	baseID, ok := blockByAlias[baseAlias]
	if !ok {
		return nil, metricLoweringError("custom-calendar time offset base metric has no SQLPlan block", nodeBase.ID)
	}
	bucket := sourceSQLPlanExpr(custom.Dataset.Name, custom.BucketExpression, expressionDialect)
	ordinal := sourceSQLPlanExpr(custom.Dataset.Name, custom.OrdinalExpression, expressionDialect)
	periods := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next), From: sourceRelation(custom.Dataset), Projections: []sqlplan.Projection{{Expr: bucket, Alias: customCalendarBucket}, {Expr: ordinal, Alias: customCalendarOrdinal}}, GroupBy: []sqlplan.Expr{bucket, ordinal}}
	var timeGroup *semanticplan.GroupBy
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		if group.Name == timeOffset.Spec.TimeDimension || unqualifiedName(group.Name) == timeOffset.Spec.TimeDimension {
			timeGroup = group
			break
		}
	}
	if timeGroup == nil {
		return nil, queryGrainLoweringError("custom-calendar time dimension must be present in query grain", nodeBase.ID)
	}
	block := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + 1), Inputs: []sqlplan.QueryInput{{Alias: baseAlias, Block: baseID, Mode: sqlplan.QueryInputCTE}, {Alias: customCalendarPeriodsCTE, Block: periods.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseAlias}, Alias: baseAlias}}
	block.Joins = append(block.Joins,
		sqlplan.Join{Kind: sqlplan.JoinInner, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: customCalendarPeriodsCTE}, Alias: customCalendarSource}, On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: baseAlias, Name: timeGroup.Name}, Operator: "=", Right: sqlplan.ColumnRef{Table: customCalendarSource, Name: customCalendarBucket}}},
		sqlplan.Join{Kind: sqlplan.JoinInner, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: customCalendarPeriodsCTE}, Alias: customCalendarTarget}, On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: customCalendarTarget, Name: customCalendarOrdinal}, Operator: "=", Right: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: customCalendarSource, Name: customCalendarOrdinal}, Operator: "+", Right: sqlplan.OpaqueExpr{SQL: fmt.Sprintf("%d", -custom.Count), Dialect: sqlplan.ANSISQLExpressionDialect}}}},
	)
	for _, group := range nodeBase.OutputGrain {
		expr := sqlplan.Expr(sqlplan.ColumnRef{Table: baseAlias, Name: group.Name})
		if group.Name == timeGroup.Name {
			expr = sqlplan.ColumnRef{Table: customCalendarTarget, Name: customCalendarBucket}
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: group.Name})
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: baseAlias, Name: timeOffset.Spec.BaseMetric}, Alias: nodeBase.ID})
	return []sqlplan.QueryBlock{periods, block}, nil
}

func offsetToGrainNodeSQLPlan(node semanticplan.SemanticPlanNode, cteNames map[string]string, blockByAlias map[string]sqlplan.QueryBlockID, next int, expressionDialect string) ([]sqlplan.QueryBlock, error) {
	offset, ok := node.(semanticplan.OffsetToGrainNode)
	nodeBase := node.NodeBase()
	if !ok || offset.OffsetPlan == nil || len(nodeBase.Inputs) != 1 {
		return nil, metricLoweringError("offset-to-grain metric requires one typed base metric and boundary plan", nodeBase.ID)
	}
	baseCTE, ok := cteNames[nodeBase.Inputs[0].NodeID]
	if !ok {
		return nil, metricLoweringError("offset-to-grain base metric has no evaluation node", nodeBase.ID)
	}
	baseID, ok := blockByAlias[baseCTE]
	if !ok {
		return nil, metricLoweringError("offset-to-grain base metric has no prior SQLPlan block", nodeBase.ID)
	}
	if offset.OffsetPlan.CustomCalendar {
		return customOffsetToGrainNodeSQLPlan(offset, baseCTE, baseID, next, expressionDialect)
	}
	var timeGroup *semanticplan.GroupBy
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		if group.Field != nil && (group.Field.Name == offset.OffsetPlan.TimeDimension || unqualifiedName(group.Name) == offset.OffsetPlan.TimeDimension) {
			timeGroup = group
			break
		}
	}
	if timeGroup == nil {
		return nil, queryGrainLoweringError("offset-to-grain time dimension must be present in query grain", nodeBase.ID)
	}
	block := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next), Inputs: []sqlplan.QueryInput{{Alias: baseCTE, Block: baseID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseCTE}, Alias: baseCTE}}
	partition := make([]sqlplan.Expr, 0, len(nodeBase.OutputGrain))
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		column := sqlplan.ColumnRef{Table: baseCTE, Name: group.Name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
		if group != timeGroup {
			partition = append(partition, column)
		}
	}
	queryTime := sqlplan.ColumnRef{Table: baseCTE, Name: timeGroup.Name}
	partition = append(partition, sqlplan.TimeGrainExpr{Grain: offset.OffsetPlan.BoundaryGrain, Expr: queryTime})
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.WindowExpr{Function: sqlplan.FunctionCallExpr{Name: "FIRST_VALUE", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: baseCTE, Name: offset.Spec.BaseMetric}}}, PartitionBy: partition, OrderBy: []sqlplan.Expr{queryTime}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}, Alias: nodeBase.ID})
	return []sqlplan.QueryBlock{block}, nil
}

func customOffsetToGrainNodeSQLPlan(offset semanticplan.OffsetToGrainNode, baseAlias string, baseID sqlplan.QueryBlockID, next int, expressionDialect string) ([]sqlplan.QueryBlock, error) {
	nodeBase := offset.Base
	if offset.OffsetPlan == nil {
		return nil, metricLoweringError("custom offset-to-grain requires a typed boundary plan", nodeBase.ID)
	}
	boundary := offset.OffsetPlan
	if boundary.Dataset.Name == "" || boundary.Dataset.Source == "" || boundary.BoundaryExpression == "" || boundary.OrdinalExpression == "" {
		return nil, metricLoweringError("custom offset-to-grain calendar mapping is incomplete", nodeBase.ID)
	}
	var timeGroup *semanticplan.GroupBy
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == boundary.TimeDimension {
			timeGroup = group
			break
		}
	}
	if timeGroup == nil || timeGroup.CustomCalendar == nil {
		return nil, metricLoweringError("custom offset-to-grain query bucket is missing", nodeBase.ID)
	}
	const mappingAlias = "__metis_offset_to_grain_periods"
	const queryBucket = "__metis_query_bucket"
	const boundaryBucket = "__metis_boundary_bucket"
	const ordinalColumn = "__metis_query_ordinal"
	queryExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: timeGroup.CustomCalendar.BucketExpression, Dialect: expressionDialect})
	boundaryExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: boundary.BoundaryExpression, Dialect: expressionDialect})
	ordinalExpr := sqlplan.Expr(sqlplan.OpaqueExpr{SQL: boundary.OrdinalExpression, Dialect: expressionDialect})
	mapping := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next), From: sourceRelation(boundary.Dataset), Projections: []sqlplan.Projection{{Expr: queryExpr, Alias: queryBucket}, {Expr: boundaryExpr, Alias: boundaryBucket}, {Expr: ordinalExpr, Alias: ordinalColumn}}, GroupBy: []sqlplan.Expr{queryExpr, boundaryExpr, ordinalExpr}}
	block := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + 1), Inputs: []sqlplan.QueryInput{{Alias: baseAlias, Block: baseID, Mode: sqlplan.QueryInputCTE}, {Alias: mappingAlias, Block: mapping.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseAlias}, Alias: baseAlias}}
	block.Joins = append(block.Joins, sqlplan.Join{Kind: sqlplan.JoinInner, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: mappingAlias}, Alias: mappingAlias}, On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: baseAlias, Name: timeGroup.Name}, Operator: "=", Right: sqlplan.ColumnRef{Table: mappingAlias, Name: queryBucket}}})
	partition := make([]sqlplan.Expr, 0, len(nodeBase.OutputGrain))
	for i := range nodeBase.OutputGrain {
		group := &nodeBase.OutputGrain[i]
		column := sqlplan.ColumnRef{Table: baseAlias, Name: group.Name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
		if group != timeGroup {
			partition = append(partition, column)
		}
	}
	partition = append(partition, sqlplan.ColumnRef{Table: mappingAlias, Name: boundaryBucket})
	window := sqlplan.WindowExpr{Function: sqlplan.FunctionCallExpr{Name: "FIRST_VALUE", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: baseAlias, Name: offset.Spec.BaseMetric}}}, PartitionBy: partition, OrderBy: []sqlplan.Expr{sqlplan.ColumnRef{Table: mappingAlias, Name: ordinalColumn}}, Frame: sqlplan.WindowRowsUnboundedPrecedingToCurrent}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: window, Alias: nodeBase.ID})
	return []sqlplan.QueryBlock{mapping, block}, nil
}
