package sqlplan

type Explanation struct {
	Root   QueryBlockID       `json:"root"`
	Blocks []BlockExplanation `json:"blocks"`
}

type BlockExplanation struct {
	ID                    QueryBlockID                       `json:"id"`
	Inputs                []InputExplanation                 `json:"inputs"`
	SourceKind            string                             `json:"source_kind"`
	Projections           []ProjectionExplanation            `json:"projections"`
	Joins                 []JoinExplanation                  `json:"joins"`
	Predicates            []PredicateExplanation             `json:"predicates"`
	BooleanPredicates     []BooleanPredicateExplanation      `json:"boolean_predicates,omitempty"`
	RelationshipExistence []RelationshipExistenceExplanation `json:"relationship_existence,omitempty"`
	GroupBy               []string                           `json:"group_by"`
	OrderBy               []string                           `json:"order_by"`
	HasLimit              bool                               `json:"has_limit"`
}

type InputExplanation struct {
	Alias string         `json:"alias"`
	Block QueryBlockID   `json:"block"`
	Mode  QueryInputMode `json:"mode"`
}

type ProjectionExplanation struct {
	Alias string `json:"alias"`
	Kind  string `json:"kind"`
}

type JoinExplanation struct {
	Kind       JoinKind `json:"kind"`
	SourceKind string   `json:"source_kind"`
	OnKind     string   `json:"on_kind,omitempty"`
}

type PredicateExplanation struct {
	Operator   string `json:"operator"`
	LeftKind   string `json:"left_kind"`
	ValueCount int    `json:"value_count"`
	Values     string `json:"values"`
}

type BooleanPredicateExplanation struct {
	Kind     string                        `json:"kind"`
	Leaf     *PredicateExplanation         `json:"leaf,omitempty"`
	Children []BooleanPredicateExplanation `json:"children,omitempty"`
}

type RelationshipExistenceExplanation struct {
	TargetSourceKind string                      `json:"target_source_kind"`
	CorrelationCount int                         `json:"correlation_count"`
	Predicate        BooleanPredicateExplanation `json:"predicate"`
}

func Explain(plan *Plan) (Explanation, error) {
	if err := Validate(plan); err != nil {
		return Explanation{}, err
	}
	out := Explanation{Root: plan.Root, Blocks: make([]BlockExplanation, len(plan.Blocks))}
	for i, block := range plan.Blocks {
		explained := BlockExplanation{
			ID:          block.ID,
			Inputs:      make([]InputExplanation, len(block.Inputs)),
			SourceKind:  relationKind(block.From),
			Projections: make([]ProjectionExplanation, len(block.Projections)),
			Joins:       make([]JoinExplanation, len(block.Joins)),
			Predicates:  make([]PredicateExplanation, len(block.Predicates)),
			GroupBy:     make([]string, len(block.GroupBy)),
			OrderBy:     make([]string, len(block.OrderBy)),
			HasLimit:    block.Limit != nil,
		}
		for j, input := range block.Inputs {
			explained.Inputs[j] = InputExplanation(input)
		}
		for j, projection := range block.Projections {
			explained.Projections[j] = ProjectionExplanation{Alias: projection.Alias, Kind: exprKind(projection.Expr)}
		}
		for j, join := range block.Joins {
			explained.Joins[j] = JoinExplanation{Kind: join.Kind, SourceKind: relationKind(join.Relation), OnKind: exprKind(join.On)}
		}
		for j, predicate := range block.Predicates {
			explained.Predicates[j] = PredicateExplanation{Operator: string(predicate.Operator), LeftKind: exprKind(predicate.Left), ValueCount: len(predicate.Values), Values: "<redacted>"}
		}
		for _, predicate := range block.BooleanPredicates {
			explained.BooleanPredicates = append(explained.BooleanPredicates, explainBooleanPredicate(predicate))
		}
		for _, predicate := range block.RelationshipExistence {
			explained.RelationshipExistence = append(explained.RelationshipExistence, RelationshipExistenceExplanation{
				TargetSourceKind: relationKind(predicate.Target),
				CorrelationCount: len(predicate.Correlations),
				Predicate:        explainBooleanPredicate(predicate.Predicate),
			})
		}
		for j, expr := range block.GroupBy {
			explained.GroupBy[j] = exprKind(expr)
		}
		for j, order := range block.OrderBy {
			explained.OrderBy[j] = exprKind(order.Expr) + ":" + string(order.Direction)
		}
		out.Blocks[i] = explained
	}
	return out, nil
}

func explainBooleanPredicate(predicate BooleanPredicate) BooleanPredicateExplanation {
	out := BooleanPredicateExplanation{Kind: string(predicate.Kind)}
	if predicate.Leaf != nil {
		leaf := PredicateExplanation{Operator: string(predicate.Leaf.Operator), LeftKind: exprKind(predicate.Leaf.Left), ValueCount: len(predicate.Leaf.Values), Values: "<redacted>"}
		out.Leaf = &leaf
	}
	for _, child := range predicate.Children {
		out.Children = append(out.Children, explainBooleanPredicate(child))
	}
	return out
}

func relationKind(relation RelationRef) string {
	if relation.FilteredSource != nil {
		return "filtered_table"
	}
	if relation.Source != nil {
		return "table"
	}
	return "input"
}

func exprKind(expr Expr) string {
	switch expr.(type) {
	case nil:
		return ""
	case OpaqueExpr:
		return "opaque"
	case ColumnRef:
		return "column"
	case BinaryExpr:
		return "binary"
	case NullOnZeroDivideExpr:
		return "null_on_zero_divide"
	case LogicalExpr:
		return "logical"
	case FunctionCallExpr:
		return "function"
	case NullTestExpr:
		return "null_test"
	case CaseExpr:
		return "case"
	case ParenthesizedExpr:
		return "parenthesized"
	case CastExpr:
		return "cast"
	case TimeGrainExpr:
		return "time_grain"
	case CalendarShiftExpr:
		return "calendar_shift"
	case LatestValueExpr:
		return "latest_value"
	case EarliestValueExpr:
		return "earliest_value"
	case WindowExpr:
		return "window"
	case RowNumberExpr:
		return "row_number"
	default:
		return "unknown"
	}
}
