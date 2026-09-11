package sqlplan

// Clone returns a fully owned copy of plan. Expression nodes are values, but
// their nested slices and predicate values still require explicit deep copies.
func Clone(plan *Plan) *Plan {
	if plan == nil {
		return nil
	}
	out := &Plan{Root: plan.Root, Blocks: make([]QueryBlock, len(plan.Blocks))}
	for i := range plan.Blocks {
		out.Blocks[i] = cloneBlock(plan.Blocks[i])
	}
	return out
}

func cloneBlock(block QueryBlock) QueryBlock {
	out := QueryBlock{
		ID:          block.ID,
		Inputs:      append([]QueryInput(nil), block.Inputs...),
		From:        cloneRelation(block.From),
		Projections: make([]Projection, len(block.Projections)),
		Joins:       make([]Join, len(block.Joins)),
		Predicates:  make([]Predicate, len(block.Predicates)),
		GroupBy:     cloneExprs(block.GroupBy),
		OrderBy:     cloneOrders(block.OrderBy),
	}
	if block.Limit != nil {
		limit := *block.Limit
		out.Limit = &limit
	}
	for i, projection := range block.Projections {
		out.Projections[i] = Projection{Expr: cloneExpr(projection.Expr), Alias: projection.Alias}
	}
	for i, join := range block.Joins {
		out.Joins[i] = Join{Relation: cloneRelation(join.Relation), On: cloneExpr(join.On), Kind: join.Kind}
	}
	for i, predicate := range block.Predicates {
		out.Predicates[i] = Predicate{
			Left:     cloneExpr(predicate.Left),
			Operator: predicate.Operator,
			Values:   cloneValues(predicate.Values),
		}
	}
	return out
}

func cloneRelation(relation RelationRef) RelationRef {
	out := RelationRef{Alias: relation.Alias}
	if relation.Source != nil {
		out.Source = &TableSource{Name: relation.Source.Name}
	}
	if relation.Input != nil {
		out.Input = &InputRef{Alias: relation.Input.Alias}
	}
	if relation.FilteredSource != nil {
		out.FilteredSource = &FilteredTableSource{Name: relation.FilteredSource.Name, Predicates: make([]Predicate, len(relation.FilteredSource.Predicates))}
		for i, p := range relation.FilteredSource.Predicates {
			out.FilteredSource.Predicates[i] = Predicate{Left: cloneExpr(p.Left), Operator: p.Operator, Values: cloneValues(p.Values)}
		}
	}
	return out
}

func cloneExprs(expressions []Expr) []Expr {
	out := make([]Expr, len(expressions))
	for i, expr := range expressions {
		out[i] = cloneExpr(expr)
	}
	return out
}

func cloneOrders(orders []Order) []Order {
	out := make([]Order, len(orders))
	for i, order := range orders {
		out[i] = Order{Expr: cloneExpr(order.Expr), Direction: order.Direction}
	}
	return out
}

func cloneExpr(expr Expr) Expr {
	switch value := expr.(type) {
	case nil:
		return nil
	case OpaqueExpr, ColumnRef:
		return value
	case BinaryExpr:
		value.Left = cloneExpr(value.Left)
		value.Right = cloneExpr(value.Right)
		return value
	case NullOnZeroDivideExpr:
		value.Numerator = cloneExpr(value.Numerator)
		value.Denominator = cloneExpr(value.Denominator)
		return value
	case LogicalExpr:
		value.Terms = cloneExprs(value.Terms)
		return value
	case FunctionCallExpr:
		value.Args = cloneExprs(value.Args)
		return value
	case NullTestExpr:
		value.Expr = cloneExpr(value.Expr)
		return value
	case CaseExpr:
		value.Branches = make([]CaseWhen, len(value.Branches))
		for i, branch := range expr.(CaseExpr).Branches {
			value.Branches[i] = CaseWhen{When: cloneExpr(branch.When), Then: cloneExpr(branch.Then)}
		}
		value.Else = cloneExpr(value.Else)
		return value
	case ParenthesizedExpr:
		value.Expr = cloneExpr(value.Expr)
		return value
	case CastExpr:
		value.Expr = cloneExpr(value.Expr)
		return value
	case TimeGrainExpr:
		value.Expr = cloneExpr(value.Expr)
		return value
	case CalendarShiftExpr:
		value.Expr = cloneExpr(value.Expr)
		return value
	case LatestValueExpr:
		value.Value = cloneExpr(value.Value)
		value.OrderBy = cloneExpr(value.OrderBy)
		value.TieBreak = cloneExpr(value.TieBreak)
		return value
	case EarliestValueExpr:
		value.Value = cloneExpr(value.Value)
		value.OrderBy = cloneExpr(value.OrderBy)
		value.TieBreak = cloneExpr(value.TieBreak)
		return value
	case WindowExpr:
		value.Function = cloneExpr(value.Function).(FunctionCallExpr)
		value.PartitionBy = cloneExprs(value.PartitionBy)
		value.OrderBy = cloneExprs(value.OrderBy)
		return value
	case RowNumberExpr:
		value.PartitionBy = cloneExprs(value.PartitionBy)
		value.OrderBy = cloneOrders(value.OrderBy)
		return value
	default:
		return value
	}
}

func cloneValues(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case []any:
		return cloneValues(typed)
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return value
	}
}
