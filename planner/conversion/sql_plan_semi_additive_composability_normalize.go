package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

// NormalizeSemiAdditiveComposableInputsToSQLPlan is the typed SQLPlan
// replacement for NormalizeSemiAdditiveComposableInputs. It gives a composed
// ordered selector a single, join-free input relation.
func NormalizeSemiAdditiveComposableInputsToSQLPlan(physical *sqlplan.Plan, semantic *semanticplan.SemanticPlan) error {
	if physical == nil || semantic == nil || len(semantic.Nodes) == 0 {
		return nil
	}
	root := sqlPlanBlock(physical, physical.Root)
	if root == nil {
		return semiAdditivePlanningError("", "semi-additive normalization requires an existing SQLPlan root block")
	}
	nodes := semanticplan.NodesByID(semantic.Nodes)
	ctes := semanticMetricRelations(semantic)
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

		consumerAlias := ctes[semi.Base.ID]
		consumer := sqlPlanInputBlock(physical, root, consumerAlias)
		if consumer == nil {
			return semiAdditivePlanningError(semi.Base.ID, "semi-additive evaluation node is missing from SQLPlan")
		}
		baseCTE := ctes[base.Base.ID]
		inputAlias := semiAdditiveStateInput(semi.Base.ID)
		if !normalizeSemiAdditiveNodeSQLPlanInputState(consumer, baseCTE, inputAlias, semi, base) {
			return semiAdditivePlanningError(semi.Base.ID, "semi-additive selector input is missing from SQLPlan")
		}
		input, err := semiAdditiveNodeStateInputSQLPlanBlock(root, consumer.ID, baseCTE, base)
		if err != nil {
			return err
		}
		input.ID = sqlplan.QueryBlockID(string(consumer.ID) + "/state_input")
		if err := insertSQLPlanRootInputBefore(root, consumerAlias, sqlplan.QueryInput{Alias: inputAlias, Block: input.ID, Mode: sqlplan.QueryInputCTE}); err != nil {
			return semiAdditivePlanningError(semi.Base.ID, err.Error())
		}
		consumer.Inputs = append(consumer.Inputs, sqlplan.QueryInput{Alias: inputAlias, Block: input.ID, Mode: sqlplan.QueryInputCTE})
		physical.Blocks = append(physical.Blocks, input)
		root = sqlPlanBlock(physical, physical.Root)
	}
	if err := topologicallyOrderSQLPlanBlocks(physical); err != nil {
		return semiAdditivePlanningError("", err.Error())
	}
	if err := sqlplan.Validate(physical); err != nil {
		return semiAdditivePlanningError("", fmt.Sprintf("semi-additive normalization produced an invalid SQLPlan: %v", err))
	}
	return nil
}

func semiAdditiveNodeStateInputSQLPlanBlock(root *sqlplan.QueryBlock, consumerID sqlplan.QueryBlockID, baseCTE string, base semanticplan.SemiAdditiveNode) (sqlplan.QueryBlock, error) {
	id := base.Base.ID
	baseInput, ok := sqlPlanInput(root, baseCTE)
	if !ok {
		return sqlplan.QueryBlock{}, semiAdditivePlanningError(id, "semi-additive base input is missing from SQLPlan")
	}
	input := sqlplan.QueryBlock{ID: sqlplan.QueryBlockID(string(consumerID) + "/state_input"), Inputs: []sqlplan.QueryInput{baseInput}, From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseCTE}, Alias: baseCTE}}
	seen := map[string]struct{}{}
	appendProjection := func(table, name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		input.Projections = append(input.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: table, Name: name}, Alias: name})
	}
	for _, group := range base.Base.OutputGrain {
		appendProjection(baseCTE, group.Name)
	}
	appendProjection(baseCTE, id)
	orderInputAlias := semiAdditiveStateOrderInput(id)
	orderInput, ok := sqlPlanInput(root, orderInputAlias)
	if !ok {
		return sqlplan.QueryBlock{}, semiAdditivePlanningError(id, "semi-additive order state is missing from SQLPlan")
	}
	input.Inputs = append(input.Inputs, orderInput)
	input.Joins = append(input.Joins, semiAdditiveStateSQLPlanJoin(baseCTE, orderInputAlias, base.Base.OutputGrain))
	appendProjection(orderInputAlias, semiAdditiveStateOrderColumn(id))
	if base.Spec.TieBreakDimension != "" {
		tieBreakInputAlias := semiAdditiveStateTieBreakInput(id)
		tieInput, ok := sqlPlanInput(root, tieBreakInputAlias)
		if !ok {
			return sqlplan.QueryBlock{}, semiAdditivePlanningError(id, "semi-additive tie-break state is missing from SQLPlan")
		}
		input.Inputs = append(input.Inputs, tieInput)
		input.Joins = append(input.Joins, semiAdditiveStateSQLPlanJoin(baseCTE, tieBreakInputAlias, base.Base.OutputGrain))
		appendProjection(tieBreakInputAlias, semiAdditiveStateTieBreakColumn(id))
	}
	return input, nil
}

func normalizeSemiAdditiveNodeSQLPlanInputState(query *sqlplan.QueryBlock, baseCTE, inputAlias string, current, prior semanticplan.SemiAdditiveNode) bool {
	if query == nil || query.From.Input == nil || query.From.Input.Alias != baseCTE {
		return false
	}
	baseID := prior.Base.ID
	orderInputAlias := semiAdditiveStateOrderInput(baseID)
	tieBreakInputAlias := semiAdditiveStateTieBreakInput(baseID)
	joins := query.Joins[:0]
	hasOrder := false
	for _, join := range query.Joins {
		alias := ""
		if join.Relation.Input != nil {
			alias = join.Relation.Input.Alias
		}
		if alias == orderInputAlias {
			hasOrder = true
			continue
		}
		if alias == tieBreakInputAlias && prior.Spec.TieBreakDimension != "" {
			continue
		}
		joins = append(joins, join)
	}
	if !hasOrder {
		return false
	}
	query.Joins = joins
	query.From = sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: inputAlias}, Alias: inputAlias}
	orderInput := semiAdditiveNodeOrderKey(current)
	tieInput := current.Spec.TieBreakDimension
	rewrite := func(column sqlplan.ColumnRef) sqlplan.Expr {
		switch column.Table {
		case baseCTE:
			column.Table = inputAlias
			if column.Name == orderInput {
				column.Name = semiAdditiveStateOrderColumn(baseID)
			} else if tieInput != "" && column.Name == tieInput {
				column.Name = semiAdditiveStateTieBreakColumn(baseID)
			}
		case orderInputAlias, tieBreakInputAlias:
			column.Table = inputAlias
		}
		return column
	}
	rewriteSQLPlanBlockExpressions(query, rewrite)
	return true
}

func sqlPlanInput(block *sqlplan.QueryBlock, alias string) (sqlplan.QueryInput, bool) {
	if block == nil {
		return sqlplan.QueryInput{}, false
	}
	for _, input := range block.Inputs {
		if input.Alias == alias {
			return input, true
		}
	}
	return sqlplan.QueryInput{}, false
}

func insertSQLPlanRootInputBefore(root *sqlplan.QueryBlock, before string, input sqlplan.QueryInput) error {
	for i := range root.Inputs {
		if root.Inputs[i].Alias != before {
			continue
		}
		inputs := make([]sqlplan.QueryInput, 0, len(root.Inputs)+1)
		inputs = append(inputs, root.Inputs[:i]...)
		inputs = append(inputs, input)
		inputs = append(inputs, root.Inputs[i:]...)
		root.Inputs = inputs
		return nil
	}
	return fmt.Errorf("semi-additive consumer input %q is missing from SQLPlan", before)
}
