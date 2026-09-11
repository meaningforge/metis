package expression

import "strings"

type Parser struct {
	lexer   *Lexer
	dialect DialectProfile
	cur     Token
	peek    Token
	lastEnd int
}

func Parse(input string, dialect DialectProfile) (Expr, error) {
	p := &Parser{lexer: NewLexer(input), dialect: dialect}
	var err error
	if p.cur, err = p.lexer.Next(); err != nil {
		return nil, err
	}
	if p.peek, err = p.lexer.Next(); err != nil {
		return nil, err
	}
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.cur.Kind != TokenEOF {
		return nil, p.parseError(ParseUnexpectedToken, p.cur, "unexpected trailing token")
	}
	return expr, nil
}

func (p *Parser) advance() error {
	p.lastEnd = p.cur.End
	p.cur = p.peek
	n, err := p.lexer.Next()
	if err != nil {
		return err
	}
	p.peek = n
	return nil
}

func (p *Parser) spanFrom(start int) SourceSpan {
	end := p.lastEnd
	if end < start {
		end = start
	}
	return SourceSpan{Start: start, End: end}
}

func (p *Parser) finish(expr Expr, start int) Expr {
	return setExprSpan(expr, p.spanFrom(start))
}

func (p *Parser) parseError(code ParseErrorCode, tok Token, message string, expected ...string) *ParseError {
	span := tok.Span()
	if tok.Kind == TokenEOF {
		code = ParseUnexpectedEOF
	}
	return &ParseError{Code: code, Message: message, Span: span, Expected: expected, Found: tok.Text}
}

func (p *Parser) parseExpr(min int) (Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		prec, rightAssoc, ok := p.infixPrecedence()
		if !ok || prec < min {
			break
		}
		op := p.cur
		if err := p.advance(); err != nil {
			return nil, err
		}
		nextMin := prec + 1
		if rightAssoc {
			nextMin = prec
		}
		left, err = p.parseInfix(left, op, nextMin)
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *Parser) parsePrefix() (Expr, error) {
	tok := p.cur
	start := tok.Pos
	if tok.Keyword("CASE") {
		return p.parseCase()
	}
	if tok.Keyword("CAST") {
		return p.parseCastFunction()
	}
	if tok.Keyword("INTERVAL") {
		return p.parseInterval()
	}
	if tok.Keyword("NULL") || tok.Keyword("TRUE") || tok.Keyword("FALSE") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.finish(&LiteralExpr{Value: tok.Text}, start), nil
	}
	if tok.Keyword("NOT") || tok.Keyword("DISTINCT") || tok.Kind == TokenPlus || tok.Kind == TokenMinus {
		if err := p.advance(); err != nil {
			return nil, err
		}
		e, err := p.parseExpr(80)
		if err != nil {
			return nil, err
		}
		return p.finish(&UnaryExpr{Op: strings.ToUpper(tok.Text), Expr: e}, start), nil
	}
	switch tok.Kind {
	case TokenIdent:
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.cur.Kind == TokenLParen {
			return p.parseCall(tok.Text, start)
		}
		return p.finish(&IdentifierExpr{Parts: []string{tok.Text}}, start), nil
	case TokenNumber, TokenString:
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.finish(&LiteralExpr{Value: tok.Text}, start), nil
	case TokenStar:
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.finish(&WildcardExpr{}, start), nil
	case TokenLParen:
		return p.parseParenthesized()
	case TokenLBracket:
		if err := p.advance(); err != nil {
			return nil, err
		}
		args, err := p.parseDelimited(TokenRBracket)
		if err != nil {
			return nil, err
		}
		return p.finish(&FunctionCallExpr{Name: "array", Args: args}, start), nil
	default:
		return nil, p.parseError(ParseUnexpectedToken, tok, "unexpected token at expression start", "identifier", "literal", "(")
	}
}

func (p *Parser) parseParenthesized() (Expr, error) {
	start := p.cur.Pos
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.cur.Kind == TokenRParen {
		return nil, p.parseError(ParseInvalidSyntax, p.cur, "empty parenthesized expression")
	}
	first, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.cur.Kind == TokenRParen {
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.finish(first, start), nil
	}
	if p.cur.Kind != TokenComma {
		return nil, p.parseError(ParseExpectedToken, p.cur, "expected closing parenthesis or tuple separator", ")", ",")
	}
	items := []Expr{first}
	for p.cur.Kind == TokenComma {
		if err := p.advance(); err != nil {
			return nil, err
		}
		item, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if p.cur.Kind != TokenRParen {
		return nil, p.parseError(ParseExpectedToken, p.cur, "unterminated tuple", ")")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p.finish(&TupleExpr{Items: items}, start), nil
}

func (p *Parser) parseCall(name string, start int) (Expr, error) {
	if err := p.advance(); err != nil {
		return nil, err
	}
	args, err := p.parseDelimited(TokenRParen)
	if err != nil {
		return nil, err
	}
	return p.finish(&FunctionCallExpr{Name: name, Args: args}, start), nil
}

func (p *Parser) parseDelimited(end TokenKind) ([]Expr, error) {
	var out []Expr
	if p.cur.Kind == end {
		return out, p.advance()
	}
	for {
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if p.cur.Kind == end {
			return out, p.advance()
		}
		if p.cur.Kind != TokenComma {
			return nil, p.parseError(ParseExpectedToken, p.cur, "expected argument separator", ",", string(end))
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
}

func (p *Parser) infixPrecedence() (int, bool, bool) {
	t := p.cur
	switch {
	case t.Kind == TokenDot:
		return 100, false, true
	case t.Kind == TokenLParen:
		return 95, false, true
	case t.Kind == TokenLBracket && p.dialect.AllowIndex:
		return 95, false, true
	case t.Kind == TokenDColon && p.dialect.AllowDoubleColonCast:
		return 92, false, true
	case t.Kind == TokenColon && p.dialect.AllowColonAccess:
		return 92, false, true
	case t.Kind == TokenStar || t.Kind == TokenSlash || t.Kind == TokenPercent:
		return 70, false, true
	case t.Kind == TokenPlus || t.Kind == TokenMinus:
		return 60, false, true
	case t.Kind == TokenEQ || t.Kind == TokenNEQ || t.Kind == TokenLT || t.Kind == TokenLTE || t.Kind == TokenGT || t.Kind == TokenGTE:
		return 50, false, true
	case t.Keyword("IN") || t.Keyword("BETWEEN") || t.Keyword("IS") || t.Keyword("LIKE") || t.Keyword("ILIKE"):
		return 45, false, true
	case t.Keyword("NOT") && (p.peek.Keyword("IN") || p.peek.Keyword("BETWEEN") || p.peek.Keyword("LIKE") || p.peek.Keyword("ILIKE")):
		return 45, false, true
	case t.Keyword("AND"):
		return 30, false, true
	case t.Keyword("OR"):
		return 20, false, true
	case t.Kind == TokenArrow && p.dialect.AllowArrow:
		return 10, true, true
	default:
		return 0, false, false
	}
}

func (p *Parser) parseInfix(left Expr, op Token, nextMin int) (Expr, error) {
	start := SpanOf(left).Start
	switch op.Kind {
	case TokenDot:
		if p.cur.Kind != TokenIdent {
			return nil, p.parseError(ParseExpectedToken, p.cur, "expected identifier after dot", "identifier")
		}
		name := p.cur.Text
		if err := p.advance(); err != nil {
			return nil, err
		}
		if path, ok := left.(*PathAccessExpr); ok {
			path.Segments = append(path.Segments, PathKeySegment{Key: name})
			return p.finish(path, start), nil
		}
		if id, ok := left.(*IdentifierExpr); ok {
			id.Parts = append(id.Parts, name)
			return p.finish(id, start), nil
		}
		return p.finish(&PathAccessExpr{Base: left, Segments: []PathSegment{PathKeySegment{Key: name}}}, start), nil
	case TokenLParen:
		args, err := p.parseDelimited(TokenRParen)
		if err != nil {
			return nil, err
		}
		return p.finish(&ApplyExpr{Callee: left, Args: args}, start), nil
	case TokenLBracket:
		idx, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if p.cur.Kind != TokenRBracket {
			return nil, p.parseError(ParseExpectedToken, p.cur, "unterminated subscript", "]")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		if path, ok := left.(*PathAccessExpr); ok {
			path.Segments = append(path.Segments, PathIndexSegment{Index: idx})
			return p.finish(path, start), nil
		}
		return p.finish(&IndexExpr{Object: left, Index: idx}, start), nil
	case TokenDColon:
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		return p.finish(&CastExpr{Expr: left, Type: typ}, start), nil
	case TokenColon:
		if p.cur.Kind != TokenIdent && p.cur.Kind != TokenString {
			return nil, p.parseError(ParseExpectedToken, p.cur, "expected value path key after colon", "identifier", "string")
		}
		key := p.cur.Text
		if err := p.advance(); err != nil {
			return nil, err
		}
		if path, ok := left.(*PathAccessExpr); ok {
			path.Segments = append(path.Segments, PathKeySegment{Key: key})
			return p.finish(path, start), nil
		}
		return p.finish(&PathAccessExpr{Base: left, Segments: []PathSegment{PathKeySegment{Key: key}}}, start), nil
	case TokenArrow:
		params, ok := lambdaParams(left)
		if !ok {
			return nil, p.parseError(ParseInvalidLambda, op, "lambda parameters must be a simple identifier or identifier tuple")
		}
		body, err := p.parseExpr(nextMin)
		if err != nil {
			return nil, err
		}
		return p.finish(&LambdaExpr{Params: params, Body: body}, start), nil
	}

	if op.Keyword("BETWEEN") || (op.Keyword("NOT") && p.cur.Keyword("BETWEEN")) {
		not := op.Keyword("NOT")
		if not {
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
		low, err := p.parseExpr(46)
		if err != nil {
			return nil, err
		}
		if !p.cur.Keyword("AND") {
			return nil, p.parseError(ParseExpectedToken, p.cur, "BETWEEN requires AND", "AND")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		high, err := p.parseExpr(46)
		if err != nil {
			return nil, err
		}
		return p.finish(&BetweenExpr{Expr: left, Not: not, Low: low, High: high}, start), nil
	}
	if op.Keyword("IN") || (op.Keyword("NOT") && p.cur.Keyword("IN")) {
		not := op.Keyword("NOT")
		if not {
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
		if p.cur.Kind != TokenLParen {
			return nil, p.parseError(ParseExpectedToken, p.cur, "IN requires a parenthesized value list", "(")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		values, err := p.parseDelimited(TokenRParen)
		if err != nil {
			return nil, err
		}
		return p.finish(&InExpr{Expr: left, Not: not, Values: values}, start), nil
	}
	if op.Keyword("IS") {
		not := false
		if p.cur.Keyword("NOT") {
			not = true
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
		if !p.cur.Keyword("NULL") {
			return nil, p.parseError(ParseExpectedToken, p.cur, "only IS [NOT] NULL is supported", "NULL")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.finish(&IsNullExpr{Expr: left, Not: not}, start), nil
	}
	if op.Keyword("NOT") && (p.cur.Keyword("LIKE") || p.cur.Keyword("ILIKE")) {
		op.Text = "NOT " + p.cur.Text
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	right, err := p.parseExpr(nextMin)
	if err != nil {
		return nil, err
	}
	return p.finish(&BinaryExpr{Left: left, Op: strings.ToUpper(op.Text), Right: right}, start), nil
}

func lambdaParams(expr Expr) ([]string, bool) {
	switch e := expr.(type) {
	case *IdentifierExpr:
		if len(e.Parts) != 1 {
			return nil, false
		}
		return []string{e.Parts[0]}, true
	case *TupleExpr:
		params := make([]string, 0, len(e.Items))
		for _, item := range e.Items {
			id, ok := item.(*IdentifierExpr)
			if !ok || len(id.Parts) != 1 {
				return nil, false
			}
			params = append(params, id.Parts[0])
		}
		return params, len(params) > 0
	default:
		return nil, false
	}
}

func (p *Parser) parseCase() (Expr, error) {
	start := p.cur.Pos
	if err := p.advance(); err != nil {
		return nil, err
	}
	var operand Expr
	var err error
	if !p.cur.Keyword("WHEN") {
		operand, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	out := &CaseExpr{Operand: operand}
	for p.cur.Keyword("WHEN") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if !p.cur.Keyword("THEN") {
			return nil, p.parseError(ParseExpectedToken, p.cur, "CASE WHEN requires THEN", "THEN")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		res, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		out.Whens = append(out.Whens, WhenExpr{Condition: cond, Result: res})
	}
	if p.cur.Keyword("ELSE") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		out.Else, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	if !p.cur.Keyword("END") {
		return nil, p.parseError(ParseExpectedToken, p.cur, "unterminated CASE expression", "END")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p.finish(out, start), nil
}

func (p *Parser) parseCastFunction() (Expr, error) {
	start := p.cur.Pos
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.cur.Kind != TokenLParen {
		return nil, p.parseError(ParseExpectedToken, p.cur, "CAST requires opening parenthesis", "(")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	e, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if !p.cur.Keyword("AS") {
		return nil, p.parseError(ParseExpectedToken, p.cur, "CAST requires AS", "AS")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	typ, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}
	if p.cur.Kind != TokenRParen {
		return nil, p.parseError(ParseExpectedToken, p.cur, "unterminated CAST", ")")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p.finish(&CastExpr{Expr: e, Type: typ}, start), nil
}

func (p *Parser) parseTypeRef() (TypeRef, error) {
	if p.cur.Kind != TokenIdent {
		return TypeRef{}, p.parseError(ParseInvalidType, p.cur, "expected cast type name", "type name")
	}
	start := p.cur.Pos
	name := p.cur.Text
	if err := p.advance(); err != nil {
		return TypeRef{}, err
	}
	out := TypeRef{Name: name}
	if p.cur.Kind != TokenLParen {
		out.Source = p.spanFrom(start)
		return out, nil
	}
	if err := p.advance(); err != nil {
		return TypeRef{}, err
	}
	if p.cur.Kind == TokenRParen {
		return TypeRef{}, p.parseError(ParseInvalidType, p.cur, "type argument list cannot be empty")
	}
	for {
		switch p.cur.Kind {
		case TokenIdent:
			nested, err := p.parseTypeRef()
			if err != nil {
				return TypeRef{}, err
			}
			out.Args = append(out.Args, TypeArg{Type: &nested})
		case TokenNumber, TokenString:
			value := p.cur.Text
			if err := p.advance(); err != nil {
				return TypeRef{}, err
			}
			out.Args = append(out.Args, TypeArg{Value: value})
		default:
			return TypeRef{}, p.parseError(ParseInvalidType, p.cur, "unsupported type argument", "type name", "literal")
		}
		if p.cur.Kind == TokenRParen {
			if err := p.advance(); err != nil {
				return TypeRef{}, err
			}
			break
		}
		if p.cur.Kind != TokenComma {
			return TypeRef{}, p.parseError(ParseExpectedToken, p.cur, "expected type argument separator", ",", ")")
		}
		if err := p.advance(); err != nil {
			return TypeRef{}, err
		}
	}
	out.Source = p.spanFrom(start)
	return out, nil
}

func (p *Parser) parseInterval() (Expr, error) {
	start := p.cur.Pos
	if err := p.advance(); err != nil {
		return nil, err
	}
	value, err := p.parseExpr(81)
	if err != nil {
		return nil, err
	}
	if p.cur.Kind != TokenIdent {
		return nil, p.parseError(ParseExpectedToken, p.cur, "INTERVAL requires a unit", "interval unit")
	}
	unit := strings.ToUpper(p.cur.Text)
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p.finish(&IntervalExpr{Value: value, Unit: unit}, start), nil
}
