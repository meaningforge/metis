package expression

type Expr interface{ exprNode() }

type IdentifierExpr struct {
	NodeMeta
	Parts []string
}

func (*IdentifierExpr) exprNode() {}

type LiteralExpr struct {
	NodeMeta
	Value string
}

func (*LiteralExpr) exprNode() {}

// WildcardExpr represents '*' in aggregate/function argument positions such as
// COUNT(*). It is syntax, not a semantic column reference.
type WildcardExpr struct{ NodeMeta }

func (*WildcardExpr) exprNode() {}

type FunctionCallExpr struct {
	NodeMeta
	Name string
	Args []Expr
}

func (*FunctionCallExpr) exprNode() {}

type ApplyExpr struct {
	NodeMeta
	Callee Expr
	Args   []Expr
}

func (*ApplyExpr) exprNode() {}

type UnaryExpr struct {
	NodeMeta
	Op   string
	Expr Expr
}

func (*UnaryExpr) exprNode() {}

type BinaryExpr struct {
	NodeMeta
	Left  Expr
	Op    string
	Right Expr
}

func (*BinaryExpr) exprNode() {}

type BetweenExpr struct {
	NodeMeta
	Expr Expr
	Not  bool
	Low  Expr
	High Expr
}

func (*BetweenExpr) exprNode() {}

type InExpr struct {
	NodeMeta
	Expr   Expr
	Not    bool
	Values []Expr
}

func (*InExpr) exprNode() {}

type IsNullExpr struct {
	NodeMeta
	Expr Expr
	Not  bool
}

func (*IsNullExpr) exprNode() {}

type WhenExpr struct {
	Condition Expr
	Result    Expr
}

type CaseExpr struct {
	NodeMeta
	Operand Expr
	Whens   []WhenExpr
	Else    Expr
}

func (*CaseExpr) exprNode() {}

type CastExpr struct {
	NodeMeta
	Expr Expr
	Type TypeRef
}

func (*CastExpr) exprNode() {}

// IntervalExpr represents the common INTERVAL <value> <unit> expression form.
// The value remains an expression so semantic references, if ever present, are
// still traversed by the reference collector.
type IntervalExpr struct {
	NodeMeta
	Value Expr
	Unit  string
}

func (*IntervalExpr) exprNode() {}

type LambdaExpr struct {
	NodeMeta
	Params []string
	Body   Expr
}

func (*LambdaExpr) exprNode() {}

// TupleExpr is used for parenthesized expression lists. In dialects that allow
// tuple lambda parameters, a tuple made exclusively of simple identifiers can
// be promoted to LambdaExpr.Params by the parser.
type TupleExpr struct {
	NodeMeta
	Items []Expr
}

func (*TupleExpr) exprNode() {}

// IndexExpr is ordinary SQL/container subscripting. It remains distinct from
// semi-structured path navigation because both the object and index expression
// may carry semantic references.
type IndexExpr struct {
	NodeMeta
	Object Expr
	Index  Expr
}

func (*IndexExpr) exprNode() {}

// PathAccessExpr represents navigation inside a semi-structured value. The
// Base is a SQL expression while Segments describe navigation within that
// value. Path segments are not relational/semantic column references.
type PathAccessExpr struct {
	NodeMeta
	Base     Expr
	Segments []PathSegment
}

func (*PathAccessExpr) exprNode() {}

type PathSegment interface{ pathSegment() }

type PathKeySegment struct{ Key string }

func (PathKeySegment) pathSegment() {}

type PathIndexSegment struct{ Index Expr }

func (PathIndexSegment) pathSegment() {}

type PathDynamicSegment struct{ Expr Expr }

func (PathDynamicSegment) pathSegment() {}
