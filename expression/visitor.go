package expression

// Visitor is the stable traversal contract for expression AST consumers.
// Enter returning false prunes the node's children; Leave is still invoked for
// nodes whose Enter returned true.
type Visitor interface {
	Enter(Expr) bool
	Leave(Expr)
}

func Walk(expr Expr, visitor Visitor) {
	if expr == nil || visitor == nil {
		return
	}
	if !visitor.Enter(expr) {
		return
	}

	switch e := expr.(type) {
	case *FunctionCallExpr:
		for _, a := range e.Args {
			Walk(a, visitor)
		}
	case *ApplyExpr:
		Walk(e.Callee, visitor)
		for _, a := range e.Args {
			Walk(a, visitor)
		}
	case *UnaryExpr:
		Walk(e.Expr, visitor)
	case *BinaryExpr:
		Walk(e.Left, visitor)
		Walk(e.Right, visitor)
	case *BetweenExpr:
		Walk(e.Expr, visitor)
		Walk(e.Low, visitor)
		Walk(e.High, visitor)
	case *InExpr:
		Walk(e.Expr, visitor)
		for _, v := range e.Values {
			Walk(v, visitor)
		}
	case *IsNullExpr:
		Walk(e.Expr, visitor)
	case *CaseExpr:
		Walk(e.Operand, visitor)
		for _, w := range e.Whens {
			Walk(w.Condition, visitor)
			Walk(w.Result, visitor)
		}
		Walk(e.Else, visitor)
	case *CastExpr:
		Walk(e.Expr, visitor)
	case *IntervalExpr:
		Walk(e.Value, visitor)
	case *LambdaExpr:
		Walk(e.Body, visitor)
	case *TupleExpr:
		for _, item := range e.Items {
			Walk(item, visitor)
		}
	case *IndexExpr:
		Walk(e.Object, visitor)
		Walk(e.Index, visitor)
	case *PathAccessExpr:
		Walk(e.Base, visitor)
		for _, segment := range e.Segments {
			switch s := segment.(type) {
			case PathIndexSegment:
				Walk(s.Index, visitor)
			case PathDynamicSegment:
				Walk(s.Expr, visitor)
			}
		}
	}

	visitor.Leave(expr)
}
