package sqlplan

import "github.com/meaningforge/metis/query"

const ANSISQLExpressionDialect = "ANSI_SQL"

type Plan struct {
	Root   QueryBlockID
	Blocks []QueryBlock
}

type QueryBlockID string

type QueryBlock struct {
	ID          QueryBlockID
	Inputs      []QueryInput
	From        RelationRef
	Projections []Projection
	Joins       []Join
	Predicates  []Predicate
	GroupBy     []Expr
	OrderBy     []Order
	Limit       *int
}

type QueryInputMode string

const (
	QueryInputCTE          QueryInputMode = "cte"
	QueryInputDerivedTable QueryInputMode = "derived_table"
)

type QueryInput struct {
	Alias string
	Block QueryBlockID
	Mode  QueryInputMode
}

type RelationRef struct {
	Source         *TableSource
	Input          *InputRef
	Alias          string
	FilteredSource *FilteredTableSource
}

// FilteredTableSource restricts a physical relation's input rows before any
// join or aggregation. Predicates are conjunctive local ColumnRefs, not general
// expressions. Renderers must never move them into the enclosing WHERE clause.
type FilteredTableSource struct {
	Name       string
	Predicates []Predicate
}

type TableSource struct{ Name string }
type InputRef struct{ Alias string }

type Projection struct {
	Expr  Expr
	Alias string
}

type JoinKind string

const (
	JoinInner     JoinKind = "inner"
	JoinFullOuter JoinKind = "full_outer"
	JoinCross     JoinKind = "cross"
)

type Join struct {
	Relation RelationRef
	On       Expr
	Kind     JoinKind
}

type Predicate struct {
	Left     Expr
	Operator query.FilterOperator
	Values   []any
}

type Order struct {
	Expr      Expr
	Direction query.SortDirection
}

type Expr interface{ sqlPlanExpr() }

// OpaqueExpr is a resolver-selected physical expression leaf. Dialect records
// the selection evidence: it must match the selected Renderer expression
// dialect or ANSI_SQL.
type OpaqueExpr struct {
	SQL     string
	Dialect string
}

func (OpaqueExpr) sqlPlanExpr() {}

type ColumnRef struct {
	Table string
	Name  string
}

func (ColumnRef) sqlPlanExpr() {}

type BinaryExpr struct {
	Left     Expr
	Operator string
	Right    Expr
}

func (BinaryExpr) sqlPlanExpr() {}

// NullOnZeroDivideExpr is the target-neutral physical division form for
// Metis-owned analytical workflows whose zero denominator is undefined. It
// renders through standard NULLIF without requiring a target-specific branch.
// Authored opaque expressions retain their authored division semantics.
type NullOnZeroDivideExpr struct {
	Numerator   Expr
	Denominator Expr
}

func (NullOnZeroDivideExpr) sqlPlanExpr() {}

type LogicalExpr struct {
	Operator string
	Terms    []Expr
}

func (LogicalExpr) sqlPlanExpr() {}

type FunctionCallExpr struct {
	Name string
	Args []Expr
}

func (FunctionCallExpr) sqlPlanExpr() {}

// NullTestExpr preserves SQL three-valued null tests as typed physical
// structure. Negated renders IS NOT NULL; the zero value renders IS NULL.
type NullTestExpr struct {
	Expr    Expr
	Negated bool
}

func (NullTestExpr) sqlPlanExpr() {}

type CaseWhen struct {
	When Expr
	Then Expr
}

// CaseExpr is the target-neutral searched CASE form. It is used when physical
// lowering must distinguish semantic states without choosing a dialect-only
// conditional function.
type CaseExpr struct {
	Branches []CaseWhen
	Else     Expr
}

func (CaseExpr) sqlPlanExpr() {}

// ParenthesizedExpr records grouping required by physical expression algebra
// without changing the historical rendering of every BinaryExpr.
type ParenthesizedExpr struct{ Expr Expr }

func (ParenthesizedExpr) sqlPlanExpr() {}

type CastType string

const (
	CastDecimal20Scale12 CastType = "DECIMAL(20,12)"
	CastDecimal38Scale18 CastType = "DECIMAL(38,18)"
)

// CastExpr carries one closed standard-SQL physical cast without exposing an
// arbitrary type string to semantic lowering.
type CastExpr struct {
	Expr Expr
	Type CastType
}

func (CastExpr) sqlPlanExpr() {}

type TimeGrainExpr struct {
	Grain query.TimeGrain
	Expr  Expr
}

func (TimeGrainExpr) sqlPlanExpr() {}

type CalendarShiftExpr struct {
	Expr  Expr
	Count int
	Unit  query.TimeGrain
}

func (CalendarShiftExpr) sqlPlanExpr() {}

type LatestValueExpr struct {
	Value    Expr
	OrderBy  Expr
	TieBreak Expr
}

func (LatestValueExpr) sqlPlanExpr() {}

type EarliestValueExpr struct {
	Value    Expr
	OrderBy  Expr
	TieBreak Expr
}

func (EarliestValueExpr) sqlPlanExpr() {}

type WindowFrame string

const (
	WindowRowsUnboundedPrecedingToCurrent WindowFrame = "rows_unbounded_preceding_to_current"
	WindowRowsPrecedingToCurrent          WindowFrame = "rows_preceding_to_current"
)

type WindowExpr struct {
	Function      FunctionCallExpr
	PartitionBy   []Expr
	OrderBy       []Expr
	Frame         WindowFrame
	PrecedingRows int
}

func (WindowExpr) sqlPlanExpr() {}

type RowNumberExpr struct {
	PartitionBy []Expr
	OrderBy     []Order
}

func (RowNumberExpr) sqlPlanExpr() {}
