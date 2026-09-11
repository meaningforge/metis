package sqlplan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

type CanonicalPlan struct {
	Root   QueryBlockID     `json:"root"`
	Blocks []CanonicalBlock `json:"blocks"`
}

type CanonicalBlock struct {
	ID          QueryBlockID          `json:"id"`
	Inputs      []QueryInput          `json:"inputs"`
	From        CanonicalRelation     `json:"from"`
	Projections []CanonicalProjection `json:"projections"`
	Joins       []CanonicalJoin       `json:"joins"`
	Predicates  []CanonicalPredicate  `json:"predicates"`
	GroupBy     []CanonicalExpr       `json:"group_by"`
	OrderBy     []CanonicalOrder      `json:"order_by"`
	Limit       *int                  `json:"limit,omitempty"`
}

type CanonicalRelation struct {
	Kind       string               `json:"kind"`
	Name       string               `json:"name"`
	Alias      string               `json:"alias"`
	Predicates []CanonicalPredicate `json:"predicates,omitempty"`
}

type CanonicalProjection struct {
	Expr  CanonicalExpr `json:"expr"`
	Alias string        `json:"alias"`
}

type CanonicalJoin struct {
	Relation CanonicalRelation `json:"relation"`
	On       *CanonicalExpr    `json:"on,omitempty"`
	Kind     JoinKind          `json:"kind"`
}

type CanonicalPredicate struct {
	Left     CanonicalExpr    `json:"left"`
	Operator string           `json:"operator"`
	Values   []CanonicalValue `json:"values"`
}

type CanonicalOrder struct {
	Expr      CanonicalExpr `json:"expr"`
	Direction string        `json:"direction"`
}

// CanonicalExpr is an explicit tagged projection of every expression field.
// It is intentionally not a serialization of the Go interface object graph.
type CanonicalExpr struct {
	Kind          string           `json:"kind"`
	SQL           string           `json:"sql,omitempty"`
	Dialect       string           `json:"dialect,omitempty"`
	Table         string           `json:"table,omitempty"`
	Name          string           `json:"name,omitempty"`
	Operator      string           `json:"operator,omitempty"`
	Grain         string           `json:"grain,omitempty"`
	Type          string           `json:"type,omitempty"`
	Count         int              `json:"count,omitempty"`
	Left          *CanonicalExpr   `json:"left,omitempty"`
	Right         *CanonicalExpr   `json:"right,omitempty"`
	Terms         []CanonicalExpr  `json:"terms,omitempty"`
	Args          []CanonicalExpr  `json:"args,omitempty"`
	Branches      []CanonicalCase  `json:"branches,omitempty"`
	Else          *CanonicalExpr   `json:"else,omitempty"`
	Negated       bool             `json:"negated,omitempty"`
	Value         *CanonicalExpr   `json:"value,omitempty"`
	OrderExpr     *CanonicalExpr   `json:"order_expr,omitempty"`
	TieBreak      *CanonicalExpr   `json:"tie_break,omitempty"`
	Function      *CanonicalExpr   `json:"function,omitempty"`
	PartitionBy   []CanonicalExpr  `json:"partition_by,omitempty"`
	OrderBy       []CanonicalExpr  `json:"order_by,omitempty"`
	DirectedOrder []CanonicalOrder `json:"directed_order,omitempty"`
	Frame         string           `json:"frame,omitempty"`
	PrecedingRows int              `json:"preceding_rows,omitempty"`
}

type CanonicalCase struct {
	When CanonicalExpr `json:"when"`
	Then CanonicalExpr `json:"then"`
}

type CanonicalValue struct {
	Kind   string           `json:"kind"`
	Scalar string           `json:"scalar,omitempty"`
	Items  []CanonicalValue `json:"items,omitempty"`
	Fields []CanonicalField `json:"fields,omitempty"`
}

type CanonicalField struct {
	Name  string         `json:"name"`
	Value CanonicalValue `json:"value"`
}

func Project(plan *Plan) (CanonicalPlan, error) {
	if err := Validate(plan); err != nil {
		return CanonicalPlan{}, err
	}
	out := CanonicalPlan{Root: plan.Root, Blocks: make([]CanonicalBlock, len(plan.Blocks))}
	for i, block := range plan.Blocks {
		projected, err := projectBlock(block)
		if err != nil {
			return CanonicalPlan{}, fmt.Errorf("project query block %q: %w", block.ID, err)
		}
		out.Blocks[i] = projected
	}
	return out, nil
}

func Fingerprint(plan *Plan) (string, error) {
	projection, err := Project(plan)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("serialize canonical SQL plan projection: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload)), nil
}

func projectBlock(block QueryBlock) (CanonicalBlock, error) {
	from, err := projectRelation(block.From)
	if err != nil {
		return CanonicalBlock{}, err
	}
	out := CanonicalBlock{ID: block.ID, Inputs: append([]QueryInput(nil), block.Inputs...), From: from}
	if block.Limit != nil {
		limit := *block.Limit
		out.Limit = &limit
	}
	out.Projections = make([]CanonicalProjection, len(block.Projections))
	for i, projection := range block.Projections {
		expr, err := projectExpr(projection.Expr)
		if err != nil {
			return CanonicalBlock{}, err
		}
		out.Projections[i] = CanonicalProjection{Expr: expr, Alias: projection.Alias}
	}
	out.Joins = make([]CanonicalJoin, len(block.Joins))
	for i, join := range block.Joins {
		relation, err := projectRelation(join.Relation)
		if err != nil {
			return CanonicalBlock{}, err
		}
		projected := CanonicalJoin{Relation: relation, Kind: join.Kind}
		if join.On != nil {
			expr, err := projectExpr(join.On)
			if err != nil {
				return CanonicalBlock{}, err
			}
			projected.On = &expr
		}
		out.Joins[i] = projected
	}
	out.Predicates = make([]CanonicalPredicate, len(block.Predicates))
	for i, predicate := range block.Predicates {
		left, err := projectExpr(predicate.Left)
		if err != nil {
			return CanonicalBlock{}, err
		}
		values := make([]CanonicalValue, len(predicate.Values))
		for j, value := range predicate.Values {
			values[j], err = projectValue(value)
			if err != nil {
				return CanonicalBlock{}, err
			}
		}
		out.Predicates[i] = CanonicalPredicate{Left: left, Operator: string(predicate.Operator), Values: values}
	}
	out.GroupBy, err = projectExprs(block.GroupBy)
	if err != nil {
		return CanonicalBlock{}, err
	}
	out.OrderBy, err = projectOrders(block.OrderBy)
	if err != nil {
		return CanonicalBlock{}, err
	}
	return out, nil
}

func projectRelation(relation RelationRef) (CanonicalRelation, error) {
	if source := relation.FilteredSource; source != nil {
		out := CanonicalRelation{Kind: "filtered_table", Name: source.Name, Alias: relation.Alias}
		for _, predicate := range source.Predicates {
			left, err := projectExpr(predicate.Left)
			if err != nil {
				return CanonicalRelation{}, err
			}
			p := CanonicalPredicate{Left: left, Operator: string(predicate.Operator)}
			for _, value := range predicate.Values {
				v, err := projectValue(value)
				if err != nil {
					return CanonicalRelation{}, err
				}
				p.Values = append(p.Values, v)
			}
			out.Predicates = append(out.Predicates, p)
		}
		return out, nil
	}
	if relation.Source != nil {
		return CanonicalRelation{Kind: "table", Name: relation.Source.Name, Alias: relation.Alias}, nil
	}
	return CanonicalRelation{Kind: "input", Name: relation.Input.Alias, Alias: relation.Alias}, nil
}

func projectExprs(expressions []Expr) ([]CanonicalExpr, error) {
	out := make([]CanonicalExpr, len(expressions))
	for i, expr := range expressions {
		projected, err := projectExpr(expr)
		if err != nil {
			return nil, err
		}
		out[i] = projected
	}
	return out, nil
}

func projectOrders(orders []Order) ([]CanonicalOrder, error) {
	out := make([]CanonicalOrder, len(orders))
	for i, order := range orders {
		expr, err := projectExpr(order.Expr)
		if err != nil {
			return nil, err
		}
		out[i] = CanonicalOrder{Expr: expr, Direction: string(order.Direction)}
	}
	return out, nil
}

func projectExpr(expr Expr) (CanonicalExpr, error) {
	switch value := expr.(type) {
	case OpaqueExpr:
		return CanonicalExpr{Kind: "opaque", SQL: value.SQL, Dialect: value.Dialect}, nil
	case ColumnRef:
		return CanonicalExpr{Kind: "column", Table: value.Table, Name: value.Name}, nil
	case BinaryExpr:
		left, err := projectExpr(value.Left)
		if err != nil {
			return CanonicalExpr{}, err
		}
		right, err := projectExpr(value.Right)
		if err != nil {
			return CanonicalExpr{}, err
		}
		return CanonicalExpr{Kind: "binary", Operator: value.Operator, Left: &left, Right: &right}, nil
	case NullOnZeroDivideExpr:
		numerator, err := projectExpr(value.Numerator)
		if err != nil {
			return CanonicalExpr{}, err
		}
		denominator, err := projectExpr(value.Denominator)
		if err != nil {
			return CanonicalExpr{}, err
		}
		return CanonicalExpr{Kind: "null_on_zero_divide", Left: &numerator, Right: &denominator}, nil
	case LogicalExpr:
		terms, err := projectExprs(value.Terms)
		return CanonicalExpr{Kind: "logical", Operator: value.Operator, Terms: terms}, err
	case FunctionCallExpr:
		args, err := projectExprs(value.Args)
		return CanonicalExpr{Kind: "function", Name: value.Name, Args: args}, err
	case NullTestExpr:
		nested, err := projectExpr(value.Expr)
		return CanonicalExpr{Kind: "null_test", Value: &nested, Negated: value.Negated}, err
	case CaseExpr:
		branches := make([]CanonicalCase, len(value.Branches))
		for i, branch := range value.Branches {
			when, err := projectExpr(branch.When)
			if err != nil {
				return CanonicalExpr{}, err
			}
			then, err := projectExpr(branch.Then)
			if err != nil {
				return CanonicalExpr{}, err
			}
			branches[i] = CanonicalCase{When: when, Then: then}
		}
		otherwise, err := projectExpr(value.Else)
		return CanonicalExpr{Kind: "case", Branches: branches, Else: &otherwise}, err
	case ParenthesizedExpr:
		nested, err := projectExpr(value.Expr)
		return CanonicalExpr{Kind: "parenthesized", Value: &nested}, err
	case CastExpr:
		nested, err := projectExpr(value.Expr)
		return CanonicalExpr{Kind: "cast", Type: string(value.Type), Value: &nested}, err
	case TimeGrainExpr:
		nested, err := projectExpr(value.Expr)
		return CanonicalExpr{Kind: "time_grain", Grain: string(value.Grain), Value: &nested}, err
	case CalendarShiftExpr:
		nested, err := projectExpr(value.Expr)
		return CanonicalExpr{Kind: "calendar_shift", Grain: string(value.Unit), Count: value.Count, Value: &nested}, err
	case LatestValueExpr:
		return projectOrderedValue("latest_value", value.Value, value.OrderBy, value.TieBreak)
	case EarliestValueExpr:
		return projectOrderedValue("earliest_value", value.Value, value.OrderBy, value.TieBreak)
	case WindowExpr:
		function, err := projectExpr(value.Function)
		if err != nil {
			return CanonicalExpr{}, err
		}
		partition, err := projectExprs(value.PartitionBy)
		if err != nil {
			return CanonicalExpr{}, err
		}
		order, err := projectExprs(value.OrderBy)
		return CanonicalExpr{Kind: "window", Function: &function, PartitionBy: partition, OrderBy: order, Frame: string(value.Frame), PrecedingRows: value.PrecedingRows}, err
	case RowNumberExpr:
		partition, err := projectExprs(value.PartitionBy)
		if err != nil {
			return CanonicalExpr{}, err
		}
		order, err := projectOrders(value.OrderBy)
		return CanonicalExpr{Kind: "row_number", PartitionBy: partition, DirectedOrder: order}, err
	default:
		return CanonicalExpr{}, fmt.Errorf("unsupported expression type %T", expr)
	}
}

func projectOrderedValue(kind string, valueExpr, orderExpr, tieExpr Expr) (CanonicalExpr, error) {
	value, err := projectExpr(valueExpr)
	if err != nil {
		return CanonicalExpr{}, err
	}
	order, err := projectExpr(orderExpr)
	if err != nil {
		return CanonicalExpr{}, err
	}
	out := CanonicalExpr{Kind: kind, Value: &value, OrderExpr: &order}
	if tieExpr != nil {
		tie, err := projectExpr(tieExpr)
		if err != nil {
			return CanonicalExpr{}, err
		}
		out.TieBreak = &tie
	}
	return out, nil
}

func projectValue(value any) (CanonicalValue, error) {
	switch typed := value.(type) {
	case nil:
		return CanonicalValue{Kind: "null"}, nil
	case string:
		return CanonicalValue{Kind: "string", Scalar: typed}, nil
	case bool:
		return CanonicalValue{Kind: "bool", Scalar: fmt.Sprintf("%t", typed)}, nil
	case int:
		return CanonicalValue{Kind: "int", Scalar: fmt.Sprintf("%d", typed)}, nil
	case int8:
		return CanonicalValue{Kind: "int8", Scalar: fmt.Sprintf("%d", typed)}, nil
	case int16:
		return CanonicalValue{Kind: "int16", Scalar: fmt.Sprintf("%d", typed)}, nil
	case int32:
		return CanonicalValue{Kind: "int32", Scalar: fmt.Sprintf("%d", typed)}, nil
	case int64:
		return CanonicalValue{Kind: "int64", Scalar: fmt.Sprintf("%d", typed)}, nil
	case uint:
		return CanonicalValue{Kind: "uint", Scalar: fmt.Sprintf("%d", typed)}, nil
	case uint8:
		return CanonicalValue{Kind: "uint8", Scalar: fmt.Sprintf("%d", typed)}, nil
	case uint16:
		return CanonicalValue{Kind: "uint16", Scalar: fmt.Sprintf("%d", typed)}, nil
	case uint32:
		return CanonicalValue{Kind: "uint32", Scalar: fmt.Sprintf("%d", typed)}, nil
	case uint64:
		return CanonicalValue{Kind: "uint64", Scalar: fmt.Sprintf("%d", typed)}, nil
	case float32:
		return CanonicalValue{Kind: "float32", Scalar: fmt.Sprintf("%g", typed)}, nil
	case float64:
		return CanonicalValue{Kind: "float64", Scalar: fmt.Sprintf("%g", typed)}, nil
	case json.Number:
		return CanonicalValue{Kind: "json_number", Scalar: typed.String()}, nil
	case []any:
		items := make([]CanonicalValue, len(typed))
		for i, item := range typed {
			projected, err := projectValue(item)
			if err != nil {
				return CanonicalValue{}, err
			}
			items[i] = projected
		}
		return CanonicalValue{Kind: "array", Items: items}, nil
	case []string:
		items := make([]CanonicalValue, len(typed))
		for i, item := range typed {
			items[i] = CanonicalValue{Kind: "string", Scalar: item}
		}
		return CanonicalValue{Kind: "array", Items: items}, nil
	case []int:
		items := make([]CanonicalValue, len(typed))
		for i, item := range typed {
			items[i] = CanonicalValue{Kind: "int", Scalar: fmt.Sprintf("%d", item)}
		}
		return CanonicalValue{Kind: "array", Items: items}, nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := make([]CanonicalField, len(keys))
		for i, key := range keys {
			projected, err := projectValue(typed[key])
			if err != nil {
				return CanonicalValue{}, err
			}
			fields[i] = CanonicalField{Name: key, Value: projected}
		}
		return CanonicalValue{Kind: "object", Fields: fields}, nil
	case map[string]string:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := make([]CanonicalField, len(keys))
		for i, key := range keys {
			fields[i] = CanonicalField{Name: key, Value: CanonicalValue{Kind: "string", Scalar: typed[key]}}
		}
		return CanonicalValue{Kind: "object", Fields: fields}, nil
	default:
		return CanonicalValue{}, fmt.Errorf("unsupported predicate value type %T", value)
	}
}
