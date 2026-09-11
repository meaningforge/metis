package conversion

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/planner/temporal"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

// PlanOwnedComposedSQLPlan constructs explicit SQLPlan blocks for the complete
// composed DAG, including advanced evaluation and calendar boundaries.
func PlanOwnedComposedSQLPlan(plan *semanticplan.SemanticPlan, expressionDialect string) (*sqlplan.Plan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, err
	}
	if !ComposedSQLPlanSupported(plan) {
		return nil, fmt.Errorf("semantic plan does not use composed SQLPlan lowering")
	}
	nodes := plan.Nodes
	if len(nodes) == 0 {
		return nil, serrors.Internal("composed semantic evaluation graph is empty", nil)
	}

	nodesByID := semanticplan.NodesByID(nodes)
	denseTargets, err := sqlPlanDenseTargets(plan, nodes)
	if err != nil {
		return nil, err
	}
	cteNames := make(map[string]string, len(nodes))
	blockByAlias := make(map[string]sqlplan.QueryBlockID, len(nodes))
	rootInputs := make([]sqlplan.QueryInput, 0, len(nodes))
	blocks := make([]sqlplan.QueryBlock, 0, len(nodes)+1)
	renderedSourceGroups := map[string]struct{}{}
	sourceGroups := semanticNodeSourceGroups(nodes)
	for i, node := range nodes {
		base := node.NodeBase()
		metricState, _ := semanticplan.NodeMetricState(node)
		if node.Kind() == semanticplan.SemanticPlanNodeSourceAggregate && metricState.ShareGroup != "" {
			source := sourceGroup(sourceGroups, metricState.ShareGroup)
			if source == nil {
				return nil, metricLoweringError("source node is not defined", base.ID)
			}
			for _, metric := range source.Metrics {
				cteNames[metric] = source.Name
			}
			if _, exists := renderedSourceGroups[source.Name]; exists {
				continue
			}
			block, err := metricSourceNodeSQLPlan(*source, nodesByID, expressionDialect)
			if err != nil {
				return nil, err
			}
			block.ID = composedSQLPlanBlockID(len(blocks))
			if dense, ok := denseTargets[source.Name]; ok {
				aliases, denseBlocks, err := densifySourceSQLPlan(plan, dense, block, len(blocks), expressionDialect)
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, denseBlocks...)
				for i, alias := range aliases {
					blockByAlias[alias] = denseBlocks[i].ID
					rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: alias, Block: denseBlocks[i].ID, Mode: sqlplan.QueryInputCTE})
				}
			} else {
				blocks = append(blocks, block)
				blockByAlias[source.Name] = block.ID
				rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: source.Name, Block: block.ID, Mode: sqlplan.QueryInputCTE})
			}
			renderedSourceGroups[source.Name] = struct{}{}
			continue
		}

		cteName := metricCTEName(i, base.ID)
		cteNames[base.ID] = cteName
		if timeOffset, ok := node.(semanticplan.TimeOffsetNode); ok && timeOffset.CustomCalendar != nil {
			nodeBlocks, err := customCalendarTimeOffsetSQLPlan(node, cteNames, blockByAlias, len(blocks), expressionDialect)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, nodeBlocks...)
			output := nodeBlocks[len(nodeBlocks)-1]
			blockByAlias[cteName] = output.ID
			rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: output.ID, Mode: sqlplan.QueryInputCTE})
			continue
		}
		if cumulative, ok := node.(semanticplan.CumulativeWindowNode); ok && cumulative.CustomCalendarGrainToDate != nil {
			nodeBlocks, err := customCalendarGrainToDateSQLPlan(node, cteNames, blockByAlias, len(blocks), expressionDialect)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, nodeBlocks...)
			output := nodeBlocks[len(nodeBlocks)-1]
			blockByAlias[cteName] = output.ID
			rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: output.ID, Mode: sqlplan.QueryInputCTE})
			continue
		}
		if node.Kind() == semanticplan.SemanticPlanNodeConversion {
			nodeBlocks, err := conversionNodeSQLPlan(node, plan.Predicates, nodesByID, len(blocks), expressionDialect)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, nodeBlocks...)
			output := nodeBlocks[len(nodeBlocks)-1]
			blockByAlias[cteName] = output.ID
			rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: output.ID, Mode: sqlplan.QueryInputCTE})
			continue
		}
		if node.Kind() == semanticplan.SemanticPlanNodeOffsetToGrain {
			nodeBlocks, err := offsetToGrainNodeSQLPlan(node, cteNames, blockByAlias, len(blocks), expressionDialect)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, nodeBlocks...)
			output := nodeBlocks[len(nodeBlocks)-1]
			blockByAlias[cteName] = output.ID
			rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: output.ID, Mode: sqlplan.QueryInputCTE})
			continue
		}
		if node.Kind() == semanticplan.SemanticPlanNodeSemiAdditiveLast || node.Kind() == semanticplan.SemanticPlanNodeSemiAdditiveFirst {
			nodeBlocks, err := semiAdditiveNodeSQLPlan(node, cteNames, blockByAlias, len(blocks), node.Kind() == semanticplan.SemanticPlanNodeSemiAdditiveFirst)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, nodeBlocks...)
			output := nodeBlocks[len(nodeBlocks)-1]
			blockByAlias[cteName] = output.ID
			rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: output.ID, Mode: sqlplan.QueryInputCTE})
			continue
		}
		block, err := basicMetricNodeSQLPlan(node, cteNames, blockByAlias, nodesByID, expressionDialect)
		if err != nil {
			return nil, err
		}
		block.ID = composedSQLPlanBlockID(len(blocks))
		if node.Kind() == semanticplan.SemanticPlanNodeSourceAggregate {
			if dense, ok := denseTargets[cteName]; ok {
				aliases, denseBlocks, err := densifySourceSQLPlan(plan, dense, block, len(blocks), expressionDialect)
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, denseBlocks...)
				for i, alias := range aliases {
					blockByAlias[alias] = denseBlocks[i].ID
					rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: alias, Block: denseBlocks[i].ID, Mode: sqlplan.QueryInputCTE})
				}
				continue
			}
		}
		blocks = append(blocks, block)
		blockByAlias[cteName] = block.ID
		rootInputs = append(rootInputs, sqlplan.QueryInput{Alias: cteName, Block: block.ID, Mode: sqlplan.QueryInputCTE})
	}

	root, err := basicComposedOutputSQLPlanBlock(plan, nodes, cteNames, rootInputs)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, root)
	physical := &sqlplan.Plan{Root: root.ID, Blocks: blocks}
	if err := sqlplan.ValidateForRenderer(physical, expressionDialect); err != nil {
		return nil, serrors.Internal("composed SQLPlan is invalid", map[string]any{"cause": err.Error()})
	}
	return sqlplan.Clone(physical), nil
}

// ComposedSQLPlanSupported reports whether a plan uses the composed strategy.
func ComposedSQLPlanSupported(plan *semanticplan.SemanticPlan) bool {
	if plan == nil || LoweringStrategyForPlan(plan) != SemanticLoweringComposed {
		return false
	}
	for _, semanticNode := range plan.Nodes {
		if err := semanticplan.ValidateNode(semanticNode); err != nil {
			return false
		}
		switch node := semanticNode.(type) {
		case semanticplan.SourceAggregateNode, semanticplan.PostAggregateNode, semanticplan.JoinAggregatesNode, semanticplan.CrossJoinAggregatesNode:
		case semanticplan.CumulativeWindowNode, semanticplan.TimeOffsetNode, semanticplan.SemiAdditiveNode:
		case semanticplan.ConversionNode:
			if node.Conversion == nil || node.PhysicalInputs == nil {
				return false
			}
		case semanticplan.OffsetToGrainNode:
			if node.OffsetPlan == nil {
				return false
			}
		default:
			return false
		}
	}
	return len(plan.Nodes) != 0
}

func basicMetricNodeSQLPlan(node semanticplan.SemanticPlanNode, cteNames map[string]string, blockByAlias map[string]sqlplan.QueryBlockID, nodesByID map[string]semanticplan.SemanticPlanNode, expressionDialect string) (sqlplan.QueryBlock, error) {
	base := node.NodeBase()
	if node.Kind() == semanticplan.SemanticPlanNodeSourceAggregate {
		return metricSourceNodesSQLPlan([]semanticplan.SemanticPlanNode{node}, expressionDialect)
	}
	inputs := make([]string, 0, len(base.Inputs))
	for _, input := range base.Inputs {
		cte, ok := cteNames[input.NodeID]
		if !ok {
			return sqlplan.QueryBlock{}, metricLoweringError("metric dependency has no prior evaluation node", input.NodeID)
		}
		inputs = append(inputs, cte)
	}
	inputs = uniqueStrings(inputs)
	if len(inputs) == 0 {
		return sqlplan.QueryBlock{}, metricLoweringError("derived metric has no evaluation inputs", base.ID)
	}
	if node.Kind() == semanticplan.SemanticPlanNodeTimeOffset {
		return timeOffsetNodeSQLPlan(node, inputs, blockByAlias)
	}
	block := sqlplan.QueryBlock{}
	if err := configureSQLPlanNodeInputs(&block, inputs, base.OutputGrain, blockByAlias); err != nil {
		return sqlplan.QueryBlock{}, err
	}
	for _, group := range base.OutputGrain {
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: coalescedSQLPlanColumn(inputs, group.Name), Alias: group.Name})
	}
	if node.Kind() == semanticplan.SemanticPlanNodeCumulativeWindow {
		expr, err := cumulativeWindowSQLPlanExpr(node, inputs, nodesByID)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: base.ID})
		return block, nil
	}
	_, resolved, ok := semanticplan.NodeMetricExpression(node)
	if !ok || !resolved.IsResolved() {
		return sqlplan.QueryBlock{}, metricLoweringError("metric expression is unresolved", base.ID)
	}
	expr, err := rawSQLPlanExpression(resolved, base.ID, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: base.ID})
	return block, nil
}

func semiAdditiveNodeSQLPlan(node semanticplan.SemanticPlanNode, cteNames map[string]string, blockByAlias map[string]sqlplan.QueryBlockID, next int, earliest bool) ([]sqlplan.QueryBlock, error) {
	semiAdditive, ok := node.(semanticplan.SemiAdditiveNode)
	base := node.NodeBase()
	if !ok || len(base.Inputs) != 1 {
		return nil, metricLoweringError("semi-additive ordered metric requires one typed base metric", base.ID)
	}
	baseCTE, ok := cteNames[base.Inputs[0].NodeID]
	if !ok {
		return nil, metricLoweringError("metric dependency has no prior evaluation node", base.Inputs[0].NodeID)
	}
	baseBlock, ok := blockByAlias[baseCTE]
	if !ok {
		return nil, metricLoweringError("semi-additive base metric has no prior SQLPlan block", base.ID)
	}
	spec := semiAdditive.Spec
	selectionGroups := make([]string, 0, len(base.OutputGrain)+len(spec.WindowGroupings))
	for _, group := range base.OutputGrain {
		selectionGroups = append(selectionGroups, group.Name)
	}
	selectionGroups = uniqueStrings(append(selectionGroups, spec.WindowGroupings...))
	orderKey := semiAdditiveNodeOrderKey(semiAdditive)

	blocks := make([]sqlplan.QueryBlock, 0, 3)
	sourceAlias := baseCTE
	sourceBlock := baseBlock
	if spec.NullPolicy == "skip" {
		const candidateAlias = "__metis_non_null_candidates"
		candidate := sqlplan.QueryBlock{
			ID:     composedSQLPlanBlockID(next + len(blocks)),
			Inputs: []sqlplan.QueryInput{{Alias: baseCTE, Block: baseBlock, Mode: sqlplan.QueryInputCTE}},
			From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseCTE}, Alias: baseCTE},
		}
		seen := map[string]struct{}{}
		appendProjection := func(name string) {
			if name == "" {
				return
			}
			if _, exists := seen[name]; exists {
				return
			}
			seen[name] = struct{}{}
			candidate.Projections = append(candidate.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: baseCTE, Name: name}, Alias: name})
		}
		for _, name := range selectionGroups {
			appendProjection(name)
		}
		appendProjection(spec.BaseMetric)
		appendProjection(orderKey)
		appendProjection(spec.TieBreakDimension)
		candidate.Predicates = append(candidate.Predicates, sqlplan.Predicate{Left: sqlplan.ColumnRef{Table: baseCTE, Name: spec.BaseMetric}, Operator: query.FilterIsNotNull})
		blocks = append(blocks, candidate)
		sourceAlias = candidateAlias
		sourceBlock = candidate.ID
	}

	selected := semiAdditiveSelectionSQLPlanBlock(semiAdditive, sourceAlias, sourceBlock, selectionGroups, orderKey, earliest)
	selected.ID = composedSQLPlanBlockID(next + len(blocks))
	if len(spec.WindowGroupings) == 0 {
		blocks = append(blocks, selected)
		return blocks, nil
	}

	const selectedAlias = "__metis_window_group_selected"
	outer := sqlplan.QueryBlock{ID: composedSQLPlanBlockID(next + len(blocks) + 1)}
	if spec.NullPolicy == "skip" {
		outer.Inputs = append(outer.Inputs, sqlplan.QueryInput{Alias: sourceAlias, Block: sourceBlock, Mode: sqlplan.QueryInputCTE})
	}
	outer.Inputs = append(outer.Inputs, sqlplan.QueryInput{Alias: selectedAlias, Block: selected.ID, Mode: sqlplan.QueryInputCTE})
	outer.From = sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: selectedAlias}, Alias: selectedAlias}
	for _, group := range base.OutputGrain {
		column := sqlplan.ColumnRef{Table: selectedAlias, Name: group.Name}
		outer.Projections = append(outer.Projections, sqlplan.Projection{Expr: column, Alias: group.Name})
		outer.GroupBy = append(outer.GroupBy, column)
	}
	rollup := strings.ToUpper(ossie.EffectiveSemiAdditiveRollupAggregation(spec))
	outer.Projections = append(outer.Projections, sqlplan.Projection{
		Expr:  sqlplan.FunctionCallExpr{Name: rollup, Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: selectedAlias, Name: base.ID}}},
		Alias: base.ID,
	})
	blocks = append(blocks, selected, outer)
	return blocks, nil
}

func semiAdditiveSelectionSQLPlanBlock(semiAdditive semanticplan.SemiAdditiveNode, sourceAlias string, sourceBlock sqlplan.QueryBlockID, groups []string, orderKey string, earliest bool) sqlplan.QueryBlock {
	spec := semiAdditive.Spec
	block := sqlplan.QueryBlock{
		Inputs: []sqlplan.QueryInput{{Alias: sourceAlias, Block: sourceBlock, Mode: sqlplan.QueryInputCTE}},
		From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: sourceAlias}, Alias: sourceAlias},
	}
	for _, name := range groups {
		column := sqlplan.ColumnRef{Table: sourceAlias, Name: name}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: column, Alias: name})
		block.GroupBy = append(block.GroupBy, column)
	}
	value := sqlplan.ColumnRef{Table: sourceAlias, Name: spec.BaseMetric}
	orderBy := sqlplan.ColumnRef{Table: sourceAlias, Name: orderKey}
	var tieBreak sqlplan.Expr
	if spec.TieBreakDimension != "" {
		tieBreak = sqlplan.ColumnRef{Table: sourceAlias, Name: spec.TieBreakDimension}
	}
	var expr sqlplan.Expr = sqlplan.LatestValueExpr{Value: value, OrderBy: orderBy, TieBreak: tieBreak}
	if earliest {
		expr = sqlplan.EarliestValueExpr{Value: value, OrderBy: orderBy, TieBreak: tieBreak}
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: semiAdditive.Base.ID})
	return block
}

func timeOffsetNodeSQLPlan(node semanticplan.SemanticPlanNode, inputs []string, blockByAlias map[string]sqlplan.QueryBlockID) (sqlplan.QueryBlock, error) {
	timeOffset, ok := node.(semanticplan.TimeOffsetNode)
	base := node.NodeBase()
	if !ok || len(base.Inputs) != 1 || len(inputs) != 1 {
		return sqlplan.QueryBlock{}, metricLoweringError("time-offset metric requires one typed base metric", base.ID)
	}
	baseCTE := inputs[0]
	baseBlock, ok := blockByAlias[baseCTE]
	if !ok {
		return sqlplan.QueryBlock{}, metricLoweringError("time-offset base metric has no prior SQLPlan block", base.ID)
	}
	block := sqlplan.QueryBlock{
		Inputs: []sqlplan.QueryInput{{Alias: baseCTE, Block: baseBlock, Mode: sqlplan.QueryInputCTE}},
		From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: baseCTE}, Alias: baseCTE},
	}
	foundTime := false
	for _, group := range base.OutputGrain {
		expr := sqlplan.Expr(sqlplan.ColumnRef{Table: baseCTE, Name: group.Name})
		if group.Name == timeOffset.Spec.TimeDimension || unqualifiedName(group.Name) == timeOffset.Spec.TimeDimension {
			expr = sqlplan.CalendarShiftExpr{Expr: expr, Count: -timeOffset.Spec.Offset.Count, Unit: query.TimeGrain(timeOffset.Spec.Offset.Unit)}
			foundTime = true
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: group.Name})
	}
	if !foundTime {
		return sqlplan.QueryBlock{}, queryGrainLoweringError("time-offset time dimension must be present in query grain", base.ID)
	}
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: baseCTE, Name: timeOffset.Spec.BaseMetric}, Alias: base.ID})
	return block, nil
}

func cumulativeWindowSQLPlanExpr(node semanticplan.SemanticPlanNode, inputs []string, nodesByID map[string]semanticplan.SemanticPlanNode) (sqlplan.Expr, error) {
	cumulative, ok := node.(semanticplan.CumulativeWindowNode)
	nodeBase := node.NodeBase()
	if !ok || len(nodeBase.Inputs) != 1 || len(inputs) != 1 {
		return nil, metricLoweringError("cumulative metric requires one typed base metric", nodeBase.ID)
	}
	base := nodeBase.Inputs[0].NodeID
	baseCTE := inputs[0]
	merge, err := cumulativeMergeOperator(node, base, nodesByID)
	if err != nil {
		return nil, err
	}
	var timeGroup *semanticplan.GroupBy
	partition := make([]sqlplan.Expr, 0)
	for i := range nodeBase.OutputGrain {
		group := nodeBase.OutputGrain[i]
		column := sqlplan.ColumnRef{Table: baseCTE, Name: group.Name}
		isCumulativeTime := group.Name == cumulative.Spec.TimeDimension || unqualifiedName(group.Name) == cumulative.Spec.TimeDimension
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == cumulative.Spec.TimeDimension {
			isCumulativeTime = true
		}
		if isCumulativeTime {
			copy := group
			timeGroup = &copy
			continue
		}
		partition = append(partition, column)
	}
	if timeGroup == nil {
		return nil, queryGrainLoweringError("cumulative time dimension must be present in query grain", nodeBase.ID)
	}
	frame := sqlplan.WindowRowsUnboundedPrecedingToCurrent
	precedingRows := 0
	orderBy := sqlplan.Expr(sqlplan.ColumnRef{Table: baseCTE, Name: timeGroup.Name})
	switch cumulative.Spec.Window.Type {
	case "rolling":
		want := query.TimeGrain(cumulative.Spec.Window.Unit)
		if timeGroup.CustomCalendar != nil {
			if timeGroup.CustomCalendar.Grain != want {
				return nil, &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "custom rolling cumulative window unit must match resolved custom query grain", Details: map[string]any{"metric": nodeBase.ID, "time_dimension": cumulative.Spec.TimeDimension, "window_unit": cumulative.Spec.Window.Unit, "query_grain": timeGroup.CustomCalendar.Grain}}
			}
			orderBy = sqlplan.ColumnRef{Table: baseCTE, Name: customDenseOrdinalColumn}
		} else if timeGroup.Grain == nil {
			return nil, queryGrainLoweringError("rolling cumulative metric requires its time dimension at an explicit query grain", nodeBase.ID)
		} else if *timeGroup.Grain != want {
			return nil, &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "rolling cumulative window unit must match query time grain", Details: map[string]any{"metric": nodeBase.ID, "time_dimension": cumulative.Spec.TimeDimension, "window_unit": cumulative.Spec.Window.Unit, "query_grain": *timeGroup.Grain}}
		}
		frame = sqlplan.WindowRowsPrecedingToCurrent
		precedingRows = cumulative.Spec.Window.Count - 1
	case "grain_to_date":
		if timeGroup.Grain == nil {
			return nil, queryGrainLoweringError("grain-to-date cumulative metric requires its time dimension at an explicit query grain", nodeBase.ID)
		}
		reset := query.TimeGrain(cumulative.Spec.Window.Unit)
		if !temporal.IsFinerGrain(*timeGroup.Grain, reset) {
			return nil, &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "grain-to-date query grain must be finer than reset boundary", Details: map[string]any{"metric": nodeBase.ID, "time_dimension": cumulative.Spec.TimeDimension, "reset_unit": reset, "query_grain": *timeGroup.Grain}}
		}
		partition = append(partition, sqlplan.TimeGrainExpr{Grain: reset, Expr: sqlplan.ColumnRef{Table: baseCTE, Name: timeGroup.Name}})
	}
	return sqlplan.WindowExpr{
		Function:      sqlplan.FunctionCallExpr{Name: merge, Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: baseCTE, Name: base}}},
		PartitionBy:   partition,
		OrderBy:       []sqlplan.Expr{orderBy},
		Frame:         frame,
		PrecedingRows: precedingRows,
	}, nil
}

func metricSourceNodeSQLPlan(source semanticSourceGroup, nodes map[string]semanticplan.SemanticPlanNode, expressionDialect string) (sqlplan.QueryBlock, error) {
	sourceNodes := make([]semanticplan.SemanticPlanNode, 0, len(source.Metrics))
	for _, metric := range source.Metrics {
		node, ok := nodes[metric]
		if !ok {
			return sqlplan.QueryBlock{}, metricLoweringError("source node metric is not defined", metric)
		}
		sourceNodes = append(sourceNodes, node)
	}
	return metricSourceNodesSQLPlan(sourceNodes, expressionDialect)
}

func metricSourceNodesSQLPlan(nodes []semanticplan.SemanticPlanNode, expressionDialect string) (sqlplan.QueryBlock, error) {
	if len(nodes) == 0 {
		return sqlplan.QueryBlock{}, metricLoweringError("source node has no metrics", "")
	}
	first := nodes[0]
	work, err := semanticplan.SourceScanWorkOfNode(first)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	shape := sqlQueryShape{Root: work.Root, Joins: work.Joins, Predicates: work.Predicates, Groups: work.OutputGrain}
	for _, group := range first.NodeBase().OutputGrain {
		resolved := group.Expression
		if group.CustomCalendar != nil {
			resolved.SourceDialect = expressionDialect
		}
		shape.Projections = append(shape.Projections, semanticplan.Projection{Name: group.Name, Kind: semanticplan.ProjectionDimension, Field: group.Field, Dataset: group.Dataset, Grain: group.Grain, Expression: resolved})
	}
	for _, node := range nodes {
		metric, resolved, ok := semanticplan.NodeMetricExpression(node)
		if !ok {
			return sqlplan.QueryBlock{}, metricLoweringError("source node metric has no typed metric node", node.NodeBase().ID)
		}
		shape.Projections = append(shape.Projections, semanticplan.Projection{Name: node.NodeBase().ID, Kind: semanticplan.ProjectionMetric, Metric: metric, Expression: resolved})
	}
	return buildSQLPlanBlockFromQueryShape(shape, expressionDialect)
}

func basicComposedOutputSQLPlanBlock(plan *semanticplan.SemanticPlan, nodes []semanticplan.SemanticPlanNode, cteNames map[string]string, inputs []sqlplan.QueryInput) (sqlplan.QueryBlock, error) {
	outputs := semanticNodeOutputMetrics(plan, nodes)
	if len(outputs) == 0 {
		return sqlplan.QueryBlock{}, serrors.Internal("composed metric evaluation has no output metrics", nil)
	}
	visibleOutputs := evaluationVisibleMetrics(plan)
	if len(visibleOutputs) == 0 {
		visibleOutputs = outputs[:1]
	}
	inputCTEs := make([]string, 0, len(visibleOutputs))
	visibleRelations := make(map[string]struct{}, len(visibleOutputs))
	for _, metric := range visibleOutputs {
		cte, ok := cteNames[metric]
		if !ok {
			return sqlplan.QueryBlock{}, metricLoweringError("metric output has no evaluation node", metric)
		}
		inputCTEs = append(inputCTEs, cte)
		visibleRelations[cte] = struct{}{}
	}
	inputCTEs = uniqueStrings(inputCTEs)
	root := sqlplan.QueryBlock{ID: "root", Inputs: append([]sqlplan.QueryInput(nil), inputs...)}
	if plan.Output.Limit != nil {
		limit := *plan.Output.Limit
		root.Limit = &limit
	}
	if err := configureSQLPlanRelations(&root, inputCTEs, plan.Groups, sqlplan.JoinFullOuter); err != nil {
		return sqlplan.QueryBlock{}, err
	}
	constraintCTEs := make([]string, 0, len(outputs)-len(visibleOutputs))
	for _, metric := range outputs {
		cte, ok := cteNames[metric]
		if !ok {
			return sqlplan.QueryBlock{}, metricLoweringError("metric output has no evaluation node", metric)
		}
		if _, visible := visibleRelations[cte]; !visible {
			constraintCTEs = append(constraintCTEs, cte)
		}
	}
	configureSQLPlanConstraintInputs(&root, inputCTEs, uniqueStrings(constraintCTEs), plan.Groups)
	for _, projection := range plan.Projections {
		switch projection.Kind {
		case semanticplan.ProjectionDimension:
			root.Projections = append(root.Projections, sqlplan.Projection{Expr: coalescedSQLPlanColumn(inputCTEs, projection.Name), Alias: projection.Name})
		case semanticplan.ProjectionMetric:
			root.Projections = append(root.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: cteNames[projection.Name], Name: projection.Name}, Alias: projection.Name})
		default:
			return sqlplan.QueryBlock{}, metricLoweringError("unsupported composed projection kind", projection.Name)
		}
	}
	for _, sort := range plan.Sorts {
		var expr sqlplan.Expr
		switch sort.Kind {
		case semanticplan.SortMetric:
			cte, ok := cteNames[sort.Name]
			if !ok {
				return sqlplan.QueryBlock{}, metricLoweringError("sort metric has no evaluation node", sort.Name)
			}
			expr = sqlplan.ColumnRef{Table: cte, Name: sort.Name}
		case semanticplan.SortDimension:
			expr = coalescedSQLPlanColumn(inputCTEs, sort.Name)
		default:
			return sqlplan.QueryBlock{}, metricLoweringError("unsupported composed sort kind", sort.Name)
		}
		root.OrderBy = append(root.OrderBy, sqlplan.Order{Expr: expr, Direction: sort.Direction})
	}
	return root, nil
}

func configureSQLPlanNodeInputs(block *sqlplan.QueryBlock, inputs []string, groups []semanticplan.GroupBy, blockByAlias map[string]sqlplan.QueryBlockID) error {
	for _, alias := range uniqueStrings(inputs) {
		id, ok := blockByAlias[alias]
		if !ok {
			return metricLoweringError("metric input has no prior SQLPlan block", alias)
		}
		block.Inputs = append(block.Inputs, sqlplan.QueryInput{Alias: alias, Block: id, Mode: sqlplan.QueryInputCTE})
	}
	return configureSQLPlanRelations(block, inputs, groups, sqlplan.JoinFullOuter)
}

func configureSQLPlanRelations(block *sqlplan.QueryBlock, inputs []string, groups []semanticplan.GroupBy, joinedKind sqlplan.JoinKind) error {
	inputs = uniqueStrings(inputs)
	if len(inputs) == 0 {
		return metricLoweringError("query block has no evaluation inputs", "")
	}
	block.From = sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: inputs[0]}, Alias: inputs[0]}
	joined := []string{inputs[0]}
	for _, input := range inputs[1:] {
		join := sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: input}, Alias: input}}
		if len(groups) == 0 {
			join.Kind = sqlplan.JoinCross
		} else {
			join.Kind = joinedKind
			terms := make([]sqlplan.Expr, 0, len(groups))
			for _, group := range groups {
				terms = append(terms, sqlplan.BinaryExpr{Left: coalescedSQLPlanColumn(joined, group.Name), Operator: "=", Right: sqlplan.ColumnRef{Table: input, Name: group.Name}})
			}
			if len(terms) == 1 {
				join.On = terms[0]
			} else {
				join.On = sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
			}
		}
		block.Joins = append(block.Joins, join)
		joined = append(joined, input)
	}
	return nil
}

func configureSQLPlanConstraintInputs(block *sqlplan.QueryBlock, visible, constraints []string, groups []semanticplan.GroupBy) {
	joined := append([]string(nil), visible...)
	for _, input := range constraints {
		join := sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: input}, Alias: input}}
		if len(groups) == 0 {
			join.Kind = sqlplan.JoinCross
		} else {
			join.Kind = sqlplan.JoinInner
			terms := make([]sqlplan.Expr, 0, len(groups))
			for _, group := range groups {
				terms = append(terms, sqlplan.BinaryExpr{Left: coalescedSQLPlanColumn(joined, group.Name), Operator: "=", Right: sqlplan.ColumnRef{Table: input, Name: group.Name}})
			}
			if len(terms) == 1 {
				join.On = terms[0]
			} else {
				join.On = sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
			}
		}
		block.Joins = append(block.Joins, join)
		joined = append(joined, input)
	}
}

func composedSQLPlanBlockID(index int) sqlplan.QueryBlockID {
	return sqlplan.QueryBlockID(fmt.Sprintf("block_%04d", index))
}
