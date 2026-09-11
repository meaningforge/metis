package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

// ApplySemiAdditiveComposabilityToSQLPlan is the typed SQLPlan replacement
// for ApplySemiAdditiveComposability. It retains selector state in sibling
// query blocks and wires composed selectors to those blocks explicitly.
func ApplySemiAdditiveComposabilityToSQLPlan(physical *sqlplan.Plan, semantic *semanticplan.SemanticPlan) error {
	if physical == nil || semantic == nil || len(semantic.Nodes) == 0 {
		return nil
	}
	root := sqlPlanBlock(physical, physical.Root)
	if root == nil {
		return semiAdditivePlanningError("", "semi-additive composability requires an existing SQLPlan root block")
	}

	nodes := semanticplan.NodesByID(semantic.Nodes)
	ctes := semanticMetricRelations(semantic)
	stateConsumers := make(map[string]struct{})
	for _, node := range semantic.Nodes {
		if err := semanticplan.ValidateNode(node); err != nil {
			return semiAdditivePlanningError("", fmt.Sprintf("validate semantic plan node: %v", err))
		}
		semi, ok := node.(semanticplan.SemiAdditiveNode)
		if !ok {
			continue
		}
		baseNode, ok := nodes[semi.Spec.BaseMetric]
		if !ok {
			continue
		}
		base, ok := baseNode.(semanticplan.SemiAdditiveNode)
		if !ok {
			continue
		}
		if err := validateSemiAdditiveComposableNodes(semi, base); err != nil {
			return err
		}
		stateConsumers[base.Base.ID] = struct{}{}
	}

	type namedBlock struct {
		alias string
		block sqlplan.QueryBlock
	}
	sidecars := make(map[string][]namedBlock)
	for _, node := range semantic.Nodes {
		semi, ok := node.(semanticplan.SemiAdditiveNode)
		id := node.NodeBase().ID
		if _, consumer := stateConsumers[id]; !consumer || !ok {
			continue
		}
		if len(semi.Spec.WindowGroupings) > 0 {
			return semiAdditivePlanningError(id, "semi-additive terminal scalar cannot expose selector state")
		}
		baseCTE := ctes[id]
		query := sqlPlanInputBlock(physical, root, baseCTE)
		if query == nil {
			return semiAdditivePlanningError(id, "semi-additive evaluation node is missing from SQLPlan")
		}
		order, hasTieBreak, err := semiAdditiveNodeSelectorStateSQLPlanBlock(physical, query.ID, semi, false)
		if err != nil {
			return err
		}
		sidecars[baseCTE] = append(sidecars[baseCTE], namedBlock{alias: semiAdditiveStateOrderInput(id), block: order})
		if hasTieBreak {
			tie, _, err := semiAdditiveNodeSelectorStateSQLPlanBlock(physical, query.ID, semi, true)
			if err != nil {
				return err
			}
			sidecars[baseCTE] = append(sidecars[baseCTE], namedBlock{alias: semiAdditiveStateTieBreakInput(id), block: tie})
		}
	}
	if len(sidecars) != 0 {
		inputs := make([]sqlplan.QueryInput, 0, len(root.Inputs)+len(sidecars)*2)
		var blocks []sqlplan.QueryBlock
		for _, input := range root.Inputs {
			inputs = append(inputs, input)
			for _, sidecar := range sidecars[input.Alias] {
				inputs = append(inputs, sqlplan.QueryInput{Alias: sidecar.alias, Block: sidecar.block.ID, Mode: sqlplan.QueryInputCTE})
				blocks = append(blocks, sidecar.block)
			}
		}
		root.Inputs = inputs
		physical.Blocks = append(physical.Blocks, blocks...)
		root = sqlPlanBlock(physical, physical.Root)
	}

	for _, node := range semantic.Nodes {
		semi, ok := node.(semanticplan.SemiAdditiveNode)
		if !ok {
			continue
		}
		baseNode, ok := nodes[semi.Spec.BaseMetric]
		if !ok {
			continue
		}
		base, ok := baseNode.(semanticplan.SemiAdditiveNode)
		if !ok {
			continue
		}
		if err := validateSemiAdditiveComposableNodes(semi, base); err != nil {
			return err
		}
		query := sqlPlanInputBlock(physical, root, ctes[semi.Base.ID])
		if query == nil {
			return semiAdditivePlanningError(semi.Base.ID, "semi-additive evaluation node is missing from SQLPlan")
		}
		rewriteSemiAdditiveNodeSQLPlanInputState(query, root, ctes[base.Base.ID], semi, base)
	}
	if err := topologicallyOrderSQLPlanBlocks(physical); err != nil {
		return semiAdditivePlanningError("", err.Error())
	}
	if err := sqlplan.Validate(physical); err != nil {
		return semiAdditivePlanningError("", fmt.Sprintf("semi-additive composability produced an invalid SQLPlan: %v", err))
	}
	return nil
}

func semiAdditiveNodeSelectorStateSQLPlanBlock(plan *sqlplan.Plan, blockID sqlplan.QueryBlockID, node semanticplan.SemiAdditiveNode, tieBreak bool) (sqlplan.QueryBlock, bool, error) {
	cloned := sqlplan.Clone(plan)
	query := sqlPlanBlock(cloned, blockID)
	id := node.Base.ID
	if query == nil {
		return sqlplan.QueryBlock{}, false, semiAdditivePlanningError(id, "semi-additive selector state requires a typed evaluation node")
	}
	for i := range query.Projections {
		if query.Projections[i].Alias != id {
			continue
		}
		var order, tie sqlplan.Expr
		switch selector := query.Projections[i].Expr.(type) {
		case sqlplan.LatestValueExpr:
			order, tie = selector.OrderBy, selector.TieBreak
			if tieBreak {
				selector.Value = tie
				query.Projections[i] = sqlplan.Projection{Expr: selector, Alias: semiAdditiveStateTieBreakColumn(id)}
			} else {
				selector.Value = order
				query.Projections[i] = sqlplan.Projection{Expr: selector, Alias: semiAdditiveStateOrderColumn(id)}
			}
		case sqlplan.EarliestValueExpr:
			order, tie = selector.OrderBy, selector.TieBreak
			if tieBreak {
				selector.Value = tie
				query.Projections[i] = sqlplan.Projection{Expr: selector, Alias: semiAdditiveStateTieBreakColumn(id)}
			} else {
				selector.Value = order
				query.Projections[i] = sqlplan.Projection{Expr: selector, Alias: semiAdditiveStateOrderColumn(id)}
			}
		default:
			return sqlplan.QueryBlock{}, false, semiAdditivePlanningError(id, "semi-additive selector state cannot be retained from a terminal scalar expression")
		}
		if tieBreak && tie == nil {
			return sqlplan.QueryBlock{}, false, semiAdditivePlanningError(id, "semi-additive selector has no tie-break state to retain")
		}
		if tieBreak {
			query.ID = sqlplan.QueryBlockID(string(blockID) + "/state_tiebreak")
		} else {
			query.ID = sqlplan.QueryBlockID(string(blockID) + "/state_order")
		}
		return *query, tie != nil, nil
	}
	return sqlplan.QueryBlock{}, false, semiAdditivePlanningError(id, "semi-additive selector output is missing from SQLPlan")
}

func rewriteSemiAdditiveNodeSQLPlanInputState(query, root *sqlplan.QueryBlock, baseCTE string, currentNode, priorNode semanticplan.SemiAdditiveNode) {
	if query == nil || root == nil || query.From.Input == nil || query.From.Input.Alias != baseCTE {
		return
	}
	current := currentNode.Spec
	prior := priorNode.Spec
	orderInput := semiAdditiveNodeOrderKey(currentNode)
	tieInput := current.TieBreakDimension
	rewrite := func(column sqlplan.ColumnRef) sqlplan.Expr {
		if column.Table == baseCTE && column.Name == orderInput {
			column.Table = semiAdditiveStateOrderInput(priorNode.Base.ID)
			column.Name = semiAdditiveStateOrderColumn(priorNode.Base.ID)
		} else if column.Table == baseCTE && tieInput != "" && column.Name == tieInput {
			column.Table = semiAdditiveStateTieBreakInput(priorNode.Base.ID)
			column.Name = semiAdditiveStateTieBreakColumn(priorNode.Base.ID)
		}
		return column
	}
	rewriteSQLPlanBlockExpressions(query, rewrite)
	orderInputAlias := semiAdditiveStateOrderInput(priorNode.Base.ID)
	appendSQLPlanInputFromRoot(query, root, orderInputAlias)
	query.Joins = append(query.Joins, semiAdditiveStateSQLPlanJoin(baseCTE, orderInputAlias, priorNode.Base.OutputGrain))
	if prior.TieBreakDimension != "" {
		tieBreakInputAlias := semiAdditiveStateTieBreakInput(priorNode.Base.ID)
		appendSQLPlanInputFromRoot(query, root, tieBreakInputAlias)
		query.Joins = append(query.Joins, semiAdditiveStateSQLPlanJoin(baseCTE, tieBreakInputAlias, priorNode.Base.OutputGrain))
	}
}

func semiAdditiveStateSQLPlanJoin(baseCTE, stateCTE string, groups []semanticplan.GroupBy) sqlplan.Join {
	join := sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: stateCTE}, Alias: stateCTE}}
	if len(groups) == 0 {
		join.Kind = sqlplan.JoinCross
		return join
	}
	join.Kind = sqlplan.JoinInner
	terms := make([]sqlplan.Expr, 0, len(groups))
	for _, group := range groups {
		terms = append(terms, sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: baseCTE, Name: group.Name}, Operator: "=", Right: sqlplan.ColumnRef{Table: stateCTE, Name: group.Name}})
	}
	if len(terms) == 1 {
		join.On = terms[0]
	} else {
		join.On = sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
	}
	return join
}

func sqlPlanInputBlock(plan *sqlplan.Plan, owner *sqlplan.QueryBlock, alias string) *sqlplan.QueryBlock {
	if owner == nil {
		return nil
	}
	for _, input := range owner.Inputs {
		if input.Alias == alias {
			return sqlPlanBlock(plan, input.Block)
		}
	}
	return nil
}

func appendSQLPlanInputFromRoot(block, root *sqlplan.QueryBlock, alias string) {
	for _, existing := range block.Inputs {
		if existing.Alias == alias {
			return
		}
	}
	for _, input := range root.Inputs {
		if input.Alias == alias {
			block.Inputs = append(block.Inputs, input)
			return
		}
	}
}

func topologicallyOrderSQLPlanBlocks(plan *sqlplan.Plan) error {
	byID := make(map[sqlplan.QueryBlockID]sqlplan.QueryBlock, len(plan.Blocks))
	for _, block := range plan.Blocks {
		byID[block.ID] = block
	}
	state := make(map[sqlplan.QueryBlockID]uint8, len(plan.Blocks))
	ordered := make([]sqlplan.QueryBlock, 0, len(plan.Blocks))
	var visit func(sqlplan.QueryBlockID) error
	visit = func(id sqlplan.QueryBlockID) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("SQLPlan query block dependency cycle at %q", id)
		case 2:
			return nil
		}
		block, ok := byID[id]
		if !ok {
			return fmt.Errorf("SQLPlan query block %q is missing", id)
		}
		state[id] = 1
		for _, input := range block.Inputs {
			if err := visit(input.Block); err != nil {
				return err
			}
		}
		state[id] = 2
		ordered = append(ordered, block)
		return nil
	}
	if err := visit(plan.Root); err != nil {
		return err
	}
	if len(ordered) != len(plan.Blocks) {
		return fmt.Errorf("SQLPlan contains unreachable query blocks")
	}
	plan.Blocks = ordered
	return nil
}

func rewriteSQLPlanBlockExpressions(block *sqlplan.QueryBlock, rewrite func(sqlplan.ColumnRef) sqlplan.Expr) {
	for i := range block.Projections {
		block.Projections[i].Expr = rewriteSQLPlanColumns(block.Projections[i].Expr, rewrite)
	}
	for i := range block.Joins {
		block.Joins[i].On = rewriteSQLPlanColumns(block.Joins[i].On, rewrite)
	}
	for i := range block.Predicates {
		block.Predicates[i].Left = rewriteSQLPlanColumns(block.Predicates[i].Left, rewrite)
	}
	for i := range block.GroupBy {
		block.GroupBy[i] = rewriteSQLPlanColumns(block.GroupBy[i], rewrite)
	}
	for i := range block.OrderBy {
		block.OrderBy[i].Expr = rewriteSQLPlanColumns(block.OrderBy[i].Expr, rewrite)
	}
}

func rewriteSQLPlanColumns(expr sqlplan.Expr, rewrite func(sqlplan.ColumnRef) sqlplan.Expr) sqlplan.Expr {
	switch value := expr.(type) {
	case nil, sqlplan.OpaqueExpr:
		return value
	case sqlplan.ColumnRef:
		return rewrite(value)
	case sqlplan.BinaryExpr:
		value.Left = rewriteSQLPlanColumns(value.Left, rewrite)
		value.Right = rewriteSQLPlanColumns(value.Right, rewrite)
		return value
	case sqlplan.NullOnZeroDivideExpr:
		value.Numerator = rewriteSQLPlanColumns(value.Numerator, rewrite)
		value.Denominator = rewriteSQLPlanColumns(value.Denominator, rewrite)
		return value
	case sqlplan.LogicalExpr:
		for i := range value.Terms {
			value.Terms[i] = rewriteSQLPlanColumns(value.Terms[i], rewrite)
		}
		return value
	case sqlplan.FunctionCallExpr:
		for i := range value.Args {
			value.Args[i] = rewriteSQLPlanColumns(value.Args[i], rewrite)
		}
		return value
	case sqlplan.TimeGrainExpr:
		value.Expr = rewriteSQLPlanColumns(value.Expr, rewrite)
		return value
	case sqlplan.CalendarShiftExpr:
		value.Expr = rewriteSQLPlanColumns(value.Expr, rewrite)
		return value
	case sqlplan.LatestValueExpr:
		value.Value = rewriteSQLPlanColumns(value.Value, rewrite)
		value.OrderBy = rewriteSQLPlanColumns(value.OrderBy, rewrite)
		value.TieBreak = rewriteSQLPlanColumns(value.TieBreak, rewrite)
		return value
	case sqlplan.EarliestValueExpr:
		value.Value = rewriteSQLPlanColumns(value.Value, rewrite)
		value.OrderBy = rewriteSQLPlanColumns(value.OrderBy, rewrite)
		value.TieBreak = rewriteSQLPlanColumns(value.TieBreak, rewrite)
		return value
	case sqlplan.WindowExpr:
		for i := range value.Function.Args {
			value.Function.Args[i] = rewriteSQLPlanColumns(value.Function.Args[i], rewrite)
		}
		for i := range value.PartitionBy {
			value.PartitionBy[i] = rewriteSQLPlanColumns(value.PartitionBy[i], rewrite)
		}
		for i := range value.OrderBy {
			value.OrderBy[i] = rewriteSQLPlanColumns(value.OrderBy[i], rewrite)
		}
		return value
	case sqlplan.RowNumberExpr:
		for i := range value.PartitionBy {
			value.PartitionBy[i] = rewriteSQLPlanColumns(value.PartitionBy[i], rewrite)
		}
		for i := range value.OrderBy {
			value.OrderBy[i].Expr = rewriteSQLPlanColumns(value.OrderBy[i].Expr, rewrite)
		}
		return value
	default:
		return expr
	}
}
