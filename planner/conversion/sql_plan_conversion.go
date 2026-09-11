package conversion

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

func conversionNodeSQLPlan(node semanticplan.SemanticPlanNode, predicates []semanticplan.Predicate, nodesByID map[string]semanticplan.SemanticPlanNode, next int, expressionDialect string) ([]sqlplan.QueryBlock, error) {
	conversion, ok := node.(semanticplan.ConversionNode)
	nodeBase := node.NodeBase()
	if !ok || conversion.Conversion == nil || conversion.PhysicalInputs == nil {
		return nil, metricLoweringError("conversion metric requires typed semantic and physical plans", nodeBase.ID)
	}
	dependencies := semanticPlanNodeInputIDs(nodeBase.Inputs)
	if len(dependencies) != 2 || !conversionDependenciesMatch(dependencies, conversion.Spec) {
		return nil, metricLoweringError("conversion metric dependencies do not match its declared inputs", nodeBase.ID)
	}
	if conversion.Conversion.Assignment != semanticplan.ConversionAssignmentNearestPrecedingBase || conversion.Conversion.CandidateMatch.KeepRank != 1 || !conversion.Conversion.CandidateMatch.AggregateBaseIndependently {
		return nil, metricLoweringError("conversion candidate assignment contract is incomplete", nodeBase.ID)
	}
	baseDefinition, conversionDefinition, err := conversionInputDefinitionPredicates(conversion, nodesByID, predicates)
	if err != nil {
		return nil, err
	}
	basePredicates := append(append([]semanticplan.Predicate(nil), predicates...), baseDefinition...)
	base, err := conversionBasePopulationSQLPlan(conversion, basePredicates, expressionDialect)
	if err != nil {
		return nil, err
	}
	base.ID = composedSQLPlanBlockID(next)
	candidates, err := conversionCandidatesSQLPlan(conversion, basePredicates, conversionDefinition, expressionDialect)
	if err != nil {
		return nil, err
	}
	candidates.ID = composedSQLPlanBlockID(next + 1)
	assigned := conversionAssignedSQLPlan(conversion, candidates.ID)
	assigned.ID = composedSQLPlanBlockID(next + 2)
	outer, err := conversionOutputSQLPlan(conversion, base.ID, candidates.ID, assigned.ID)
	if err != nil {
		return nil, err
	}
	outer.ID = composedSQLPlanBlockID(next + 3)
	return []sqlplan.QueryBlock{base, candidates, assigned, outer}, nil
}

func conversionBasePopulationSQLPlan(conversion semanticplan.ConversionNode, predicates []semanticplan.Predicate, expressionDialect string) (sqlplan.QueryBlock, error) {
	nodeBase := conversion.Base
	if conversion.PhysicalInputs == nil {
		return sqlplan.QueryBlock{}, metricLoweringError("conversion metric requires typed physical inputs", nodeBase.ID)
	}
	physical := conversion.PhysicalInputs
	block := sqlplan.QueryBlock{From: sourceRelation(physical.BaseDataset)}
	for _, group := range nodeBase.OutputGrain {
		expr, err := fieldSQLPlanExpr(group.Dataset, group.Expression, group.Grain, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: group.Name})
		block.GroupBy = append(block.GroupBy, expr)
	}
	if err := applyConversionSQLPlanPredicates(&block, predicates, expressionDialect); err != nil {
		return sqlplan.QueryBlock{}, err
	}
	value, err := conversionEventRowValueSQLPlanExpr(physical.BaseValue, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{value}}, Alias: conversionBaseValueColumn})
	return block, nil
}

func conversionCandidatesSQLPlan(conversion semanticplan.ConversionNode, basePredicates, conversionPredicates []semanticplan.Predicate, expressionDialect string) (sqlplan.QueryBlock, error) {
	nodeBase := conversion.Base
	if conversion.Conversion == nil || conversion.PhysicalInputs == nil {
		return sqlplan.QueryBlock{}, metricLoweringError("conversion candidate matching requires typed semantic and physical plans", nodeBase.ID)
	}
	semantic, physical := conversion.Conversion, conversion.PhysicalInputs
	block := sqlplan.QueryBlock{From: sourceRelation(physical.BaseDataset)}
	onTerms := make([]sqlplan.Expr, 0, len(semantic.CandidateMatch.Equality)+2)
	for _, pair := range semantic.CandidateMatch.Equality {
		left, err := conversionPhysicalSQLPlanExpr(*physical, pair.Base, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		right, err := conversionPhysicalSQLPlanExpr(*physical, pair.Conversion, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		onTerms = append(onTerms, sqlplan.BinaryExpr{Left: left, Operator: "=", Right: right})
	}
	baseTime, err := conversionPhysicalSQLPlanExpr(*physical, semantic.CandidateMatch.BaseTime, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	conversionTime, err := conversionPhysicalSQLPlanExpr(*physical, semantic.CandidateMatch.ConversionTime, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	onTerms = append(onTerms, sqlplan.BinaryExpr{Left: baseTime, Operator: "<=", Right: conversionTime})
	if window := semantic.CandidateMatch.Window; window != nil {
		onTerms = append(onTerms, sqlplan.BinaryExpr{Left: baseTime, Operator: ">=", Right: sqlplan.CalendarShiftExpr{Expr: conversionTime, Count: -window.Count, Unit: query.TimeGrain(window.Unit)}})
	}
	var on sqlplan.Expr = sqlplan.LogicalExpr{Operator: "AND", Terms: onTerms}
	if len(onTerms) == 1 {
		on = onTerms[0]
	}
	block.Joins = append(block.Joins, sqlplan.Join{Kind: sqlplan.JoinInner, Relation: sourceRelation(physical.ConversionDataset), On: on})
	if err := applyConversionSQLPlanPredicates(&block, append(basePredicates, conversionPredicates...), expressionDialect); err != nil {
		return sqlplan.QueryBlock{}, err
	}
	for _, group := range nodeBase.OutputGrain {
		expr, err := fieldSQLPlanExpr(group.Dataset, group.Expression, group.Grain, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: group.Name})
	}
	value, err := conversionEventRowValueSQLPlanExpr(physical.ConversionValue, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: value, Alias: conversionEventValueColumn})
	rank, err := conversionCandidateRankSQLPlanExpr(semantic, physical, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: rank, Alias: conversionRankColumn})
	return block, nil
}

func conversionAssignedSQLPlan(conversion semanticplan.ConversionNode, candidates sqlplan.QueryBlockID) sqlplan.QueryBlock {
	nodeBase := conversion.Base
	block := sqlplan.QueryBlock{Inputs: []sqlplan.QueryInput{{Alias: conversionCandidatesCTE, Block: candidates, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: conversionCandidatesCTE}, Alias: conversionCandidatesCTE}}
	for _, group := range nodeBase.OutputGrain {
		column := sqlplan.ColumnRef{Table: conversionCandidatesCTE, Name: group.Name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
		block.GroupBy = append(block.GroupBy, column)
	}
	block.Predicates = append(block.Predicates, sqlplan.Predicate{Left: sqlplan.ColumnRef{Table: conversionCandidatesCTE, Name: conversionRankColumn}, Operator: query.FilterEQ, Values: []any{1}})
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: conversionCandidatesCTE, Name: conversionEventValueColumn}}}, Alias: conversionAssignedColumn})
	return block
}

func conversionOutputSQLPlan(conversion semanticplan.ConversionNode, base, candidates, assigned sqlplan.QueryBlockID) (sqlplan.QueryBlock, error) {
	nodeBase := conversion.Base
	block := sqlplan.QueryBlock{Inputs: []sqlplan.QueryInput{{Alias: conversionBasePopulationCTE, Block: base, Mode: sqlplan.QueryInputCTE}, {Alias: conversionCandidatesCTE, Block: candidates, Mode: sqlplan.QueryInputCTE}, {Alias: conversionAssignedCTE, Block: assigned, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: conversionBasePopulationCTE}, Alias: conversionBasePopulationCTE}}
	var on sqlplan.Expr = sqlplan.OpaqueExpr{SQL: "TRUE", Dialect: sqlplan.ANSISQLExpressionDialect}
	if len(nodeBase.OutputGrain) != 0 {
		terms := make([]sqlplan.Expr, 0, len(nodeBase.OutputGrain))
		for _, group := range nodeBase.OutputGrain {
			terms = append(terms, sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: conversionBasePopulationCTE, Name: group.Name}, Operator: "=", Right: sqlplan.ColumnRef{Table: conversionAssignedCTE, Name: group.Name}})
		}
		on = terms[0]
		if len(terms) > 1 {
			on = sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
		}
	}
	block.Joins = append(block.Joins, sqlplan.Join{Kind: sqlplan.JoinFullOuter, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: conversionAssignedCTE}, Alias: conversionAssignedCTE}, On: on})
	for _, group := range nodeBase.OutputGrain {
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: conversionBasePopulationCTE, Name: group.Name}, sqlplan.ColumnRef{Table: conversionAssignedCTE, Name: group.Name}}}, Alias: group.Name})
	}
	assignedValue := sqlplan.Expr(sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: conversionAssignedCTE, Name: conversionAssignedColumn}, sqlplan.OpaqueExpr{SQL: "0", Dialect: sqlplan.ANSISQLExpressionDialect}}})
	metric := assignedValue
	switch conversion.Spec.Calculation {
	case ossie.ConversionCalculationConversions:
	case ossie.ConversionCalculationConversionRate:
		metric = sqlplan.NullOnZeroDivideExpr{Numerator: assignedValue, Denominator: sqlplan.ColumnRef{Table: conversionBasePopulationCTE, Name: conversionBaseValueColumn}}
	default:
		return sqlplan.QueryBlock{}, metricLoweringError("unsupported conversion calculation", nodeBase.ID)
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: metric, Alias: nodeBase.ID})
	return block, nil
}

func applyConversionSQLPlanPredicates(block *sqlplan.QueryBlock, predicates []semanticplan.Predicate, expressionDialect string) error {
	for _, predicate := range predicates {
		if predicate.Field == nil {
			continue
		}
		left, err := fieldSQLPlanExpr(predicate.Dataset, predicate.Expression, nil, expressionDialect)
		if err != nil {
			return err
		}
		values, err := predicateValues(predicate.Filter.Operator, predicate.Filter.Value)
		if err != nil {
			return &serrors.Error{Code: serrors.ErrInvalidConversionMetric, Message: "invalid conversion raw-event predicate value", Details: map[string]any{"field": predicate.Filter.Field, "cause": err.Error()}}
		}
		block.Predicates = append(block.Predicates, sqlplan.Predicate{Left: left, Operator: predicate.Filter.Operator, Values: values})
	}
	return nil
}

func conversionEventRowValueSQLPlanExpr(value semanticplan.ConversionEventValuePlan, expressionDialect string) (sqlplan.Expr, error) {
	switch value.Kind {
	case semanticplan.ConversionEventValueCountRows:
		return sqlplan.OpaqueExpr{SQL: "1", Dialect: sqlplan.ANSISQLExpressionDialect}, nil
	case semanticplan.ConversionEventValueSumField:
		if value.Field == nil {
			return nil, metricLoweringError("conversion SUM event value is missing its expressionDialect-aware field", value.Metric)
		}
		return sourceSQLPlanExpr(value.Field.Dataset, value.Field.Expression, expressionDialect), nil
	default:
		return nil, metricLoweringError("unsupported conversion event row value", value.Metric)
	}
}

func conversionPhysicalSQLPlanExpr(plan semanticplan.ConversionPhysicalInputPlan, ref semanticplan.ConversionFieldRef, expressionDialect string) (sqlplan.Expr, error) {
	field, ok := conversionPhysicalField(plan, ref)
	if !ok {
		return nil, metricLoweringError("conversion candidate field has no expressionDialect-aware physical expression", ref.Name)
	}
	return sourceSQLPlanExpr(field.Dataset, field.Expression, expressionDialect), nil
}

func conversionCandidateRankSQLPlanExpr(plan *semanticplan.ConversionPlan, physical *semanticplan.ConversionPhysicalInputPlan, expressionDialect string) (sqlplan.RowNumberExpr, error) {
	if plan == nil || physical == nil || len(plan.CandidateMatch.PartitionBy) == 0 || len(plan.CandidateMatch.OrderBy) == 0 {
		return sqlplan.RowNumberExpr{}, metricLoweringError("conversion candidate rank requires semantic and physical plans", "")
	}
	rank := sqlplan.RowNumberExpr{}
	for _, ref := range plan.CandidateMatch.PartitionBy {
		expr, err := conversionPhysicalSQLPlanExpr(*physical, ref, expressionDialect)
		if err != nil {
			return sqlplan.RowNumberExpr{}, err
		}
		rank.PartitionBy = append(rank.PartitionBy, expr)
	}
	for _, order := range plan.CandidateMatch.OrderBy {
		direction := query.SortDirection(order.Direction)
		expr, err := conversionPhysicalSQLPlanExpr(*physical, order.Field, expressionDialect)
		if err != nil {
			return sqlplan.RowNumberExpr{}, err
		}
		rank.OrderBy = append(rank.OrderBy, sqlplan.Order{Expr: expr, Direction: direction})
	}
	return rank, nil
}

func sourceSQLPlanExpr(dataset, source string, expressionDialect string) sqlplan.Expr {
	if simpleIdentifier.MatchString(source) {
		return sqlplan.ColumnRef{Table: dataset, Name: source}
	}
	return sqlplan.OpaqueExpr{SQL: source, Dialect: expressionDialect}
}
