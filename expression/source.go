package expression

// SourceSpan identifies a half-open byte range [Start, End) in the original
// expression text. Byte offsets are used deliberately so callers can slice the
// original UTF-8 input without lossy rune-index conversion.
type SourceSpan struct {
	Start int
	End   int
}

func (s SourceSpan) Valid() bool { return s.Start >= 0 && s.End >= s.Start }
func (s SourceSpan) Empty() bool { return s.End <= s.Start }

func MergeSpan(a, b SourceSpan) SourceSpan {
	if !a.Valid() || a.Empty() {
		return b
	}
	if !b.Valid() || b.Empty() {
		return a
	}
	start := a.Start
	if b.Start < start {
		start = b.Start
	}
	end := a.End
	if b.End > end {
		end = b.End
	}
	return SourceSpan{Start: start, End: end}
}

// NodeMeta is embedded by expression AST nodes. Keeping source metadata in a
// common base avoids dialect-specific position plumbing and gives diagnostics,
// visitors, and future rewrites one stable source-location contract.
type NodeMeta struct {
	Source SourceSpan
}

func (m NodeMeta) Span() SourceSpan         { return m.Source }
func (m *NodeMeta) setSpan(span SourceSpan) { m.Source = span }

type SpannedExpr interface {
	Expr
	Span() SourceSpan
}

type spanSetter interface{ setSpan(SourceSpan) }

func SpanOf(expr Expr) SourceSpan {
	if expr == nil {
		return SourceSpan{}
	}
	if s, ok := expr.(interface{ Span() SourceSpan }); ok {
		return s.Span()
	}
	return SourceSpan{}
}

func setExprSpan(expr Expr, span SourceSpan) Expr {
	if s, ok := expr.(spanSetter); ok {
		s.setSpan(span)
	}
	return expr
}
