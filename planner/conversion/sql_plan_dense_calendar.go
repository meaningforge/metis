package conversion

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

type sqlPlanDenseTarget struct {
	spec   denseNodeSpec
	custom bool
}

func sqlPlanDenseTargets(plan *semanticplan.SemanticPlan, nodes []semanticplan.SemanticPlanNode) (map[string]sqlPlanDenseTarget, error) {
	out := map[string]sqlPlanDenseTarget{}
	if plan.DenseCalendar != nil {
		targets, err := denseTimeRelativeSemanticNodes(nodes)
		if err != nil {
			return nil, err
		}
		offsets, err := offsetToGrainDenseSemanticNodes(nodes, false)
		if err != nil {
			return nil, err
		}
		for name, spec := range mergeDenseNodeSpecs(targets, offsets) {
			out[name] = sqlPlanDenseTarget{spec: spec}
		}
	}
	if plan.CustomDenseCalendar != nil {
		targets, err := denseTimeRelativeSemanticNodes(nodes)
		if err != nil {
			return nil, err
		}
		offsets, err := offsetToGrainDenseSemanticNodes(nodes, true)
		if err != nil {
			return nil, err
		}
		for name, spec := range mergeDenseNodeSpecs(targets, offsets) {
			out[name] = sqlPlanDenseTarget{spec: spec, custom: true}
		}
	}
	return out, nil
}

func densifySourceSQLPlan(plan *semanticplan.SemanticPlan, target sqlPlanDenseTarget, original sqlplan.QueryBlock, next int, expressionDialect string) ([]string, []sqlplan.QueryBlock, error) {
	if target.custom {
		return densifyCustomSourceSQLPlan(plan.CustomDenseCalendar, target.spec, original, next, expressionDialect)
	}
	return densifyBuiltInSourceSQLPlan(plan.DenseCalendar, target.spec, original, next, expressionDialect)
}

func densifyBuiltInSourceSQLPlan(calendar *semanticplan.DenseCalendarPlan, spec denseNodeSpec, original sqlplan.QueryBlock, next int, dialect string) ([]string, []sqlplan.QueryBlock, error) {
	if calendar == nil {
		return nil, nil, serrors.Internal("dense calendar source group is incomplete", nil)
	}
	timeGroup, otherGroups := splitDenseGroups(spec.OutputGrain, calendar)
	if timeGroup == nil {
		return nil, nil, serrors.Internal("dense calendar requires a matching canonical time group", map[string]any{"source_group": spec.Name})
	}
	sparseName, calendarName := spec.Name+"__sparse", spec.Name+"__calendar"
	domainName, gridName := spec.Name+"__domain", spec.Name+"__grid"
	original.ID = composedSQLPlanBlockID(next)
	blocks := []sqlplan.QueryBlock{original}
	aliases := []string{sparseName}
	calendarExpr := sqlplan.TimeGrainExpr{Grain: calendar.Grain, Expr: sqlplan.OpaqueExpr{SQL: calendar.TimeExpression, Dialect: dialect}}
	periods := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + 1), From: sourceRelation(calendar.Dataset), Projections: []sqlplan.Projection{{Expr: calendarExpr, Alias: timeGroup.Name}}, GroupBy: []sqlplan.Expr{calendarExpr}}
	if spec.HasTimeOffset && !spec.HasCumulative {
		for _, predicate := range calendar.ReadPredicates {
			values, err := predicateValues(predicate.Filter.Operator, predicate.Filter.Value)
			if err != nil {
				return nil, nil, err
			}
			periods.Predicates = append(periods.Predicates, sqlplan.Predicate{Left: calendarExpr, Operator: predicate.Filter.Operator, Values: values})
		}
	}
	blocks = append(blocks, periods)
	aliases = append(aliases, calendarName)
	gridAlias, gridID := calendarName, periods.ID
	if len(otherGroups) > 0 {
		domain := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + len(blocks)), Inputs: []sqlplan.QueryInput{{Alias: sparseName, Block: original.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: sparseName}, Alias: sparseName}}
		for _, group := range otherGroups {
			column := sqlplan.ColumnRef{Table: sparseName, Name: group.Name}
			domain.Projections = append(domain.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
			domain.GroupBy = append(domain.GroupBy, column)
		}
		blocks = append(blocks, domain)
		aliases = append(aliases, domainName)
		grid := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + len(blocks)), Inputs: []sqlplan.QueryInput{{Alias: domainName, Block: domain.ID, Mode: sqlplan.QueryInputCTE}, {Alias: calendarName, Block: periods.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: domainName}, Alias: domainName}}
		for _, group := range otherGroups {
			grid.Projections = append(grid.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: domainName, Name: group.Name}, Alias: group.Name})
		}
		grid.Projections = append(grid.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: calendarName, Name: timeGroup.Name}, Alias: timeGroup.Name})
		grid.Joins = append(grid.Joins, sqlplan.Join{Kind: sqlplan.JoinCross, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: calendarName}, Alias: calendarName}})
		blocks = append(blocks, grid)
		aliases = append(aliases, gridName)
		gridAlias, gridID = gridName, grid.ID
	}
	dense := denseJoinSQLPlanBlock(spec, sparseName, original.ID, gridAlias, gridID, "")
	dense.ID = composedSQLPlanBlockID(next + len(blocks))
	blocks = append(blocks, dense)
	aliases = append(aliases, spec.Name)
	return aliases, blocks, nil
}

func densifyCustomSourceSQLPlan(calendar *semanticplan.CustomDenseCalendarPlan, spec denseNodeSpec, original sqlplan.QueryBlock, next int, dialect string) ([]string, []sqlplan.QueryBlock, error) {
	if calendar == nil || calendar.BucketExpression == "" || calendar.OrdinalExpression == "" {
		return nil, nil, serrors.Internal("custom dense calendar source group is incomplete", nil)
	}
	timeGroup, otherGroups := splitCustomDenseGroups(spec.OutputGrain, calendar)
	if timeGroup == nil {
		return nil, nil, serrors.Internal("custom dense calendar requires a matching custom time group", map[string]any{"source_group": spec.Name})
	}
	sparseName, periodsName := spec.Name+"__sparse", spec.Name+"__periods"
	domainName, gridName := spec.Name+"__domain", spec.Name+"__grid"
	original.ID = composedSQLPlanBlockID(next)
	blocks := []sqlplan.QueryBlock{original}
	aliases := []string{sparseName}
	bucket := sourceSQLPlanExpr(calendar.Dataset.Name, calendar.BucketExpression, dialect)
	ordinal := sourceSQLPlanExpr(calendar.Dataset.Name, calendar.OrdinalExpression, dialect)
	periods := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + 1), From: sourceRelation(calendar.Dataset), Projections: []sqlplan.Projection{{Expr: bucket, Alias: timeGroup.Name}, {Expr: ordinal, Alias: customDenseOrdinalColumn}}, GroupBy: []sqlplan.Expr{bucket, ordinal}}
	blocks = append(blocks, periods)
	aliases = append(aliases, periodsName)
	gridAlias, gridID := periodsName, periods.ID
	if len(otherGroups) > 0 {
		domain := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + len(blocks)), Inputs: []sqlplan.QueryInput{{Alias: sparseName, Block: original.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: sparseName}, Alias: sparseName}}
		for _, group := range otherGroups {
			column := sqlplan.ColumnRef{Table: sparseName, Name: group.Name}
			domain.Projections = append(domain.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
			domain.GroupBy = append(domain.GroupBy, column)
		}
		blocks = append(blocks, domain)
		aliases = append(aliases, domainName)
		grid := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + len(blocks)), Inputs: []sqlplan.QueryInput{{Alias: domainName, Block: domain.ID, Mode: sqlplan.QueryInputCTE}, {Alias: periodsName, Block: periods.ID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: domainName}, Alias: domainName}}
		for _, group := range otherGroups {
			grid.Projections = append(grid.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: domainName, Name: group.Name}, Alias: group.Name})
		}
		grid.Projections = append(grid.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: periodsName, Name: timeGroup.Name}, Alias: timeGroup.Name}, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: periodsName, Name: customDenseOrdinalColumn}, Alias: customDenseOrdinalColumn})
		grid.Joins = append(grid.Joins, sqlplan.Join{Kind: sqlplan.JoinCross, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: periodsName}, Alias: periodsName}})
		blocks = append(blocks, grid)
		aliases = append(aliases, gridName)
		gridAlias, gridID = gridName, grid.ID
	}
	dense := denseJoinSQLPlanBlock(spec, sparseName, original.ID, gridAlias, gridID, customDenseOrdinalColumn)
	dense.ID = composedSQLPlanBlockID(next + len(blocks))
	blocks = append(blocks, dense)
	aliases = append(aliases, spec.Name)
	return aliases, blocks, nil
}

func denseJoinSQLPlanBlock(spec denseNodeSpec, sparseAlias string, sparseID sqlplan.QueryBlockID, gridAlias string, gridID sqlplan.QueryBlockID, ordinal string) sqlplan.QueryBlock {
	block := sqlplan.QueryBlock{Inputs: []sqlplan.QueryInput{{Alias: gridAlias, Block: gridID, Mode: sqlplan.QueryInputCTE}, {Alias: sparseAlias, Block: sparseID, Mode: sqlplan.QueryInputCTE}}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: gridAlias}, Alias: gridAlias}}
	terms := make([]sqlplan.Expr, 0, len(spec.OutputGrain))
	for _, group := range spec.OutputGrain {
		grid, sparse := sqlplan.ColumnRef{Table: gridAlias, Name: group.Name}, sqlplan.ColumnRef{Table: sparseAlias, Name: group.Name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{grid, sparse}}, Alias: group.Name})
		terms = append(terms, sqlplan.BinaryExpr{Left: grid, Operator: "=", Right: sparse})
	}
	if ordinal != "" {
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: gridAlias, Name: ordinal}, Alias: ordinal})
	}
	for _, metric := range spec.Metrics {
		expr := sqlplan.Expr(sqlplan.ColumnRef{Table: sparseAlias, Name: metric})
		if _, fill := spec.FillMetrics[metric]; fill {
			expr = zeroFilledSQLPlanExpr(expr)
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: metric})
	}
	var on sqlplan.Expr = terms[0]
	if len(terms) > 1 {
		on = sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
	}
	block.Joins = append(block.Joins, sqlplan.Join{Kind: sqlplan.JoinFullOuter, Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: sparseAlias}, Alias: sparseAlias}, On: on})
	return block
}
