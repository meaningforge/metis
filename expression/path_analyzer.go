package expression

import "fmt"

func (a *SemanticAnalyzer) analyzePathAccess(e *PathAccessExpr, insideAggregate bool) (TypedExpression, error) {
	base, err := a.analyze(e.Base, insideAggregate)
	if err != nil {
		return TypedExpression{}, err
	}

	baseType := base.Type
	out := base
	steps := make([]PathSemanticStep, 0, len(e.Segments))
	for _, segment := range e.Segments {
		switch p := segment.(type) {
		case PathKeySegment:
			steps = append(steps, PathSemanticStep{Kind: PathSegmentKey, Key: p.Key})
		case PathIndexSegment:
			operand, err := a.analyze(p.Index, insideAggregate)
			if err != nil {
				return TypedExpression{}, err
			}
			out = combineTyped(out, operand)
			steps = append(steps, PathSemanticStep{Kind: PathSegmentIndex, OperandType: operand.Type})
		case PathDynamicSegment:
			operand, err := a.analyze(p.Expr, insideAggregate)
			if err != nil {
				return TypedExpression{}, err
			}
			out = combineTyped(out, operand)
			steps = append(steps, PathSemanticStep{Kind: PathSegmentDynamic, OperandType: operand.Type})
		default:
			return TypedExpression{}, &SemanticError{Code: ErrInvalidPathAccess, Message: "unsupported path segment", Span: SpanOf(e)}
		}
	}

	resolved, err := ResolvePathAccess(baseType, steps)
	if err != nil {
		pathErr, ok := err.(*PathResolutionError)
		if !ok {
			return TypedExpression{}, err
		}
		return TypedExpression{}, &SemanticError{
			Code:    ErrInvalidPathAccess,
			Message: fmt.Sprintf("invalid path access at segment %d: %s", pathErr.Ordinal, pathErr.Failure),
			Span:    pathFailureSpan(e, pathErr.Ordinal),
		}
	}

	out.Expr = e
	out.Type = resolved.ResultType
	out.Nullability = resolved.Nullability
	out.PathResolutions = append(out.PathResolutions, PathResolutionEvidence{Resolution: resolved, Span: SpanOf(e)})
	return out, nil
}

func pathFailureSpan(expr *PathAccessExpr, ordinal int) SourceSpan {
	if ordinal < 0 || ordinal >= len(expr.Segments) {
		return SpanOf(expr)
	}
	switch segment := expr.Segments[ordinal].(type) {
	case PathIndexSegment:
		return SpanOf(segment.Index)
	case PathDynamicSegment:
		return SpanOf(segment.Expr)
	default:
		return SpanOf(expr)
	}
}
