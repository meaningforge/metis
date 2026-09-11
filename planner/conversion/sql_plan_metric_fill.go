package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

// ApplyMetricFillPoliciesToSQLPlan preserves opaque derived SQL by inserting a typed
// aligned-input query block rather than rewriting identifiers inside the leaf.
func ApplyMetricFillPoliciesToSQLPlan(physical *sqlplan.Plan, semantic *semanticplan.SemanticPlan) error {
	if physical == nil || semantic == nil {
		return nil
	}
	if err := semanticplan.ValidateSemanticPlanDAG(semantic); err != nil {
		return metricFillPlanningError("", err)
	}
	zeroFill := map[string]struct{}{}
	for _, node := range semantic.Nodes {
		base := node.NodeBase()
		metric := semanticplan.NodeMetric(node)
		if metric == nil {
			continue
		}
		enabled, err := metricUsesZeroFill(metric)
		if err != nil {
			return metricFillPlanningError(base.ID, err)
		}
		if enabled {
			zeroFill[base.ID] = struct{}{}
		}
	}
	for _, projection := range semantic.Projections {
		if projection.Kind != semanticplan.ProjectionMetric || projection.Metric == nil {
			continue
		}
		enabled, err := metricUsesZeroFill(projection.Metric)
		if err != nil {
			return metricFillPlanningError(projection.Name, err)
		}
		if enabled {
			zeroFill[projection.Name] = struct{}{}
		}
	}
	if len(zeroFill) == 0 {
		return nil
	}

	if len(semantic.Nodes) != 0 {
		cteByMetric := semanticMetricRelations(semantic)
		for _, node := range semantic.Nodes {
			base := node.NodeBase()
			block, err := sqlPlanBlockForInputAlias(physical, cteByMetric[base.ID])
			if err != nil {
				return metricFillPlanningError(base.ID, err)
			}
			if block == nil {
				continue
			}
			dependencyFill := map[string]struct{}{}
			for _, input := range base.Inputs {
				if _, ok := zeroFill[input.NodeID]; ok {
					dependencyFill[input.NodeID] = struct{}{}
				}
			}
			if len(dependencyFill) != 0 {
				if selectAliasUsesOpaqueSQLPlanExpression(block, base.ID) {
					if err := wrapRawDerivedSQLPlanWithAlignedInputs(physical, block.ID, node, cteByMetric, dependencyFill); err != nil {
						return metricFillPlanningError(base.ID, err)
					}
					block = sqlPlanBlock(physical, block.ID)
				} else {
					applyFillToSQLPlanBlockExpressions(block, dependencyFill)
				}
			}
			if _, ok := zeroFill[base.ID]; ok {
				wrapSQLPlanProjectionAliasWithZero(block, base.ID)
			}
		}
	}

	root := sqlPlanBlock(physical, physical.Root)
	if root == nil {
		return metricFillPlanningError("", fmt.Errorf("SQLPlan root block is missing"))
	}
	for i := range root.Projections {
		if _, ok := zeroFill[root.Projections[i].Alias]; ok {
			root.Projections[i].Expr = zeroFilledSQLPlanExpr(root.Projections[i].Expr)
		}
	}
	for i := range root.OrderBy {
		root.OrderBy[i].Expr = applyFillToSQLPlanExpr(root.OrderBy[i].Expr, zeroFill)
	}
	for i := range root.Predicates {
		root.Predicates[i].Left = applyFillToSQLPlanExpr(root.Predicates[i].Left, zeroFill)
	}
	if err := sqlplan.Validate(physical); err != nil {
		return metricFillPlanningError("", fmt.Errorf("metric fill rewrite produced an invalid SQLPlan: %w", err))
	}
	return nil
}

func sqlPlanBlockForInputAlias(plan *sqlplan.Plan, alias string) (*sqlplan.QueryBlock, error) {
	if plan == nil || alias == "" {
		return nil, nil
	}
	var id sqlplan.QueryBlockID
	for _, block := range plan.Blocks {
		for _, input := range block.Inputs {
			if input.Alias != alias {
				continue
			}
			if id != "" && id != input.Block {
				return nil, fmt.Errorf("SQLPlan input alias %q identifies multiple blocks", alias)
			}
			id = input.Block
		}
	}
	return sqlPlanBlock(plan, id), nil
}

func selectAliasUsesOpaqueSQLPlanExpression(block *sqlplan.QueryBlock, alias string) bool {
	if block == nil {
		return false
	}
	for _, projection := range block.Projections {
		if projection.Alias != alias {
			continue
		}
		_, ok := projection.Expr.(sqlplan.OpaqueExpr)
		return ok
	}
	return false
}

func wrapRawDerivedSQLPlanWithAlignedInputs(plan *sqlplan.Plan, blockID sqlplan.QueryBlockID, node semanticplan.SemanticPlanNode, cteByMetric map[string]string, fill map[string]struct{}) error {
	base := node.NodeBase()
	index := -1
	for i := range plan.Blocks {
		if plan.Blocks[i].ID == blockID {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("derived metric SQLPlan block is missing")
	}
	query := plan.Blocks[index]
	if len(query.Predicates) != 0 || len(query.GroupBy) != 0 || len(query.OrderBy) != 0 || query.Limit != nil {
		return fmt.Errorf("cannot safely establish fill boundary around derived metric with additional query clauses")
	}
	metricProjection := -1
	for i, projection := range query.Projections {
		if projection.Alias != base.ID {
			continue
		}
		if _, ok := projection.Expr.(sqlplan.OpaqueExpr); !ok {
			return fmt.Errorf("derived metric expression is not opaque physical SQL")
		}
		metricProjection = i
		break
	}
	if metricProjection < 0 {
		return fmt.Errorf("derived metric projection is missing")
	}

	alignedID := sqlplan.QueryBlockID(string(query.ID) + "/fill_aligned_inputs")
	if sqlPlanBlock(plan, alignedID) != nil {
		return fmt.Errorf("derived metric aligned-input block %q already exists", alignedID)
	}
	aligned := sqlplan.QueryBlock{ID: alignedID, Inputs: query.Inputs, From: query.From, Joins: query.Joins}
	for i, projection := range query.Projections {
		if i != metricProjection {
			aligned.Projections = append(aligned.Projections, projection)
		}
	}
	dependencies := make([]string, 0, len(base.Inputs))
	for _, input := range base.Inputs {
		dependencies = append(dependencies, input.NodeID)
	}
	for _, dependency := range dependencies {
		cteName, ok := cteByMetric[dependency]
		if !ok || cteName == "" {
			return fmt.Errorf("metric dependency %q has no evaluation input", dependency)
		}
		expr := sqlplan.Expr(sqlplan.ColumnRef{Table: cteName, Name: dependency})
		if _, ok := fill[dependency]; ok {
			expr = zeroFilledSQLPlanExpr(expr)
		}
		aligned.Projections = append(aligned.Projections, sqlplan.Projection{Expr: expr, Alias: dependency})
	}

	originalMetric := query.Projections[metricProjection]
	query.Inputs = []sqlplan.QueryInput{{Alias: metricFillAlignedInputsCTE, Block: alignedID, Mode: sqlplan.QueryInputCTE}}
	query.From = sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: metricFillAlignedInputsCTE}, Alias: metricFillAlignedInputsCTE}
	query.Joins = nil
	query.Projections = nil
	for _, projection := range aligned.Projections {
		if containsString(dependencies, projection.Alias) {
			continue
		}
		query.Projections = append(query.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: metricFillAlignedInputsCTE, Name: projection.Alias}, Alias: projection.Alias})
	}
	query.Projections = append(query.Projections, originalMetric)

	blocks := make([]sqlplan.QueryBlock, 0, len(plan.Blocks)+1)
	blocks = append(blocks, plan.Blocks[:index]...)
	blocks = append(blocks, aligned, query)
	blocks = append(blocks, plan.Blocks[index+1:]...)
	plan.Blocks = blocks
	return nil
}

func wrapSQLPlanProjectionAliasWithZero(block *sqlplan.QueryBlock, alias string) {
	if block == nil {
		return
	}
	for i := range block.Projections {
		if block.Projections[i].Alias == alias {
			block.Projections[i].Expr = zeroFilledSQLPlanExpr(block.Projections[i].Expr)
		}
	}
}

func zeroFilledSQLPlanExpr(expr sqlplan.Expr) sqlplan.Expr {
	return sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{expr, sqlplan.OpaqueExpr{SQL: "0", Dialect: sqlplan.ANSISQLExpressionDialect}}}
}

func applyFillToSQLPlanBlockExpressions(block *sqlplan.QueryBlock, fill map[string]struct{}) {
	if block == nil || len(fill) == 0 {
		return
	}
	for i := range block.Projections {
		block.Projections[i].Expr = applyFillToSQLPlanExpr(block.Projections[i].Expr, fill)
	}
	for i := range block.Joins {
		block.Joins[i].On = applyFillToSQLPlanExpr(block.Joins[i].On, fill)
	}
	for i := range block.Predicates {
		block.Predicates[i].Left = applyFillToSQLPlanExpr(block.Predicates[i].Left, fill)
	}
	for i := range block.GroupBy {
		block.GroupBy[i] = applyFillToSQLPlanExpr(block.GroupBy[i], fill)
	}
	for i := range block.OrderBy {
		block.OrderBy[i].Expr = applyFillToSQLPlanExpr(block.OrderBy[i].Expr, fill)
	}
}

func applyFillToSQLPlanExpr(expr sqlplan.Expr, fill map[string]struct{}) sqlplan.Expr {
	switch value := expr.(type) {
	case nil:
		return nil
	case sqlplan.OpaqueExpr:
		return value
	case sqlplan.ColumnRef:
		if _, ok := fill[value.Name]; ok {
			return zeroFilledSQLPlanExpr(value)
		}
		return value
	case sqlplan.BinaryExpr:
		value.Left = applyFillToSQLPlanExpr(value.Left, fill)
		value.Right = applyFillToSQLPlanExpr(value.Right, fill)
		return value
	case sqlplan.NullOnZeroDivideExpr:
		value.Numerator = applyFillToSQLPlanExpr(value.Numerator, fill)
		value.Denominator = applyFillToSQLPlanExpr(value.Denominator, fill)
		return value
	case sqlplan.LogicalExpr:
		for i := range value.Terms {
			value.Terms[i] = applyFillToSQLPlanExpr(value.Terms[i], fill)
		}
		return value
	case sqlplan.FunctionCallExpr:
		for i := range value.Args {
			value.Args[i] = applyFillToSQLPlanExpr(value.Args[i], fill)
		}
		return value
	case sqlplan.TimeGrainExpr:
		value.Expr = applyFillToSQLPlanExpr(value.Expr, fill)
		return value
	case sqlplan.CalendarShiftExpr:
		value.Expr = applyFillToSQLPlanExpr(value.Expr, fill)
		return value
	case sqlplan.LatestValueExpr:
		value.Value = applyFillToSQLPlanExpr(value.Value, fill)
		value.OrderBy = applyFillToSQLPlanExpr(value.OrderBy, fill)
		value.TieBreak = applyFillToSQLPlanExpr(value.TieBreak, fill)
		return value
	case sqlplan.EarliestValueExpr:
		value.Value = applyFillToSQLPlanExpr(value.Value, fill)
		value.OrderBy = applyFillToSQLPlanExpr(value.OrderBy, fill)
		value.TieBreak = applyFillToSQLPlanExpr(value.TieBreak, fill)
		return value
	case sqlplan.WindowExpr:
		for i := range value.Function.Args {
			value.Function.Args[i] = applyFillToSQLPlanExpr(value.Function.Args[i], fill)
		}
		for i := range value.PartitionBy {
			value.PartitionBy[i] = applyFillToSQLPlanExpr(value.PartitionBy[i], fill)
		}
		for i := range value.OrderBy {
			value.OrderBy[i] = applyFillToSQLPlanExpr(value.OrderBy[i], fill)
		}
		return value
	case sqlplan.RowNumberExpr:
		for i := range value.PartitionBy {
			value.PartitionBy[i] = applyFillToSQLPlanExpr(value.PartitionBy[i], fill)
		}
		for i := range value.OrderBy {
			value.OrderBy[i].Expr = applyFillToSQLPlanExpr(value.OrderBy[i].Expr, fill)
		}
		return value
	default:
		return expr
	}
}
