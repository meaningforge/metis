package expression

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

type Lexer struct {
	input string
	pos   int
}

func NewLexer(input string) *Lexer { return &Lexer{input: input} }

func (l *Lexer) Next() (Token, error) {
	for l.pos < len(l.input) {
		r, size := utf8.DecodeRuneInString(l.input[l.pos:])
		if !unicode.IsSpace(r) {
			break
		}
		l.pos += size
	}
	if l.pos >= len(l.input) {
		return Token{Kind: TokenEOF, Pos: l.pos, End: l.pos}, nil
	}
	start := l.pos
	if tok, ok := l.multi(); ok {
		return tok, nil
	}
	r, size := utf8.DecodeRuneInString(l.input[l.pos:])
	if isIdentStart(r) || r == '`' || r == '"' {
		return l.ident()
	}
	if unicode.IsDigit(r) {
		l.pos += size
		for l.pos < len(l.input) {
			r, s := utf8.DecodeRuneInString(l.input[l.pos:])
			if !unicode.IsDigit(r) && r != '.' {
				break
			}
			l.pos += s
		}
		return Token{Kind: TokenNumber, Text: l.input[start:l.pos], Pos: start, End: l.pos}, nil
	}
	if r == '\'' {
		l.pos += size
		for l.pos < len(l.input) {
			r, s := utf8.DecodeRuneInString(l.input[l.pos:])
			l.pos += s
			if r == '\'' {
				if l.pos < len(l.input) && l.input[l.pos] == '\'' {
					l.pos++
					continue
				}
				return Token{Kind: TokenString, Text: l.input[start:l.pos], Pos: start, End: l.pos}, nil
			}
		}
		return Token{}, fmt.Errorf("unterminated string at %d", start)
	}
	l.pos += size
	kind := map[rune]TokenKind{
		'(': TokenLParen, ')': TokenRParen, '[': TokenLBracket, ']': TokenRBracket,
		',': TokenComma, '.': TokenDot, ':': TokenColon, '?': TokenQuestion,
		'+': TokenPlus, '-': TokenMinus, '*': TokenStar, '/': TokenSlash, '%': TokenPercent,
		'=': TokenEQ, '<': TokenLT, '>': TokenGT,
	}[r]
	if kind == "" {
		return Token{}, fmt.Errorf("unexpected character %q at %d", r, start)
	}
	return Token{Kind: kind, Text: string(r), Pos: start, End: l.pos}, nil
}

func (l *Lexer) multi() (Token, bool) {
	pairs := []struct {
		text string
		kind TokenKind
	}{
		{"::", TokenDColon}, {"->", TokenArrow}, {"!=", TokenNEQ}, {"<>", TokenNEQ}, {"<=", TokenLTE}, {">=", TokenGTE},
	}
	for _, p := range pairs {
		if len(l.input)-l.pos >= len(p.text) && l.input[l.pos:l.pos+len(p.text)] == p.text {
			start := l.pos
			l.pos += len(p.text)
			return Token{Kind: p.kind, Text: p.text, Pos: start, End: l.pos}, true
		}
	}
	return Token{}, false
}

func (l *Lexer) ident() (Token, error) {
	start := l.pos
	r, size := utf8.DecodeRuneInString(l.input[l.pos:])
	if r == '`' || r == '"' {
		quote := r
		l.pos += size
		contentStart := l.pos
		for l.pos < len(l.input) {
			r, s := utf8.DecodeRuneInString(l.input[l.pos:])
			if r == quote {
				text := l.input[contentStart:l.pos]
				l.pos += s
				return Token{Kind: TokenIdent, Text: text, Pos: start, End: l.pos}, nil
			}
			l.pos += s
		}
		return Token{}, fmt.Errorf("unterminated quoted identifier at %d", start)
	}
	l.pos += size
	for l.pos < len(l.input) {
		r, s := utf8.DecodeRuneInString(l.input[l.pos:])
		if !isIdentPart(r) {
			break
		}
		l.pos += s
	}
	return Token{Kind: TokenIdent, Text: l.input[start:l.pos], Pos: start, End: l.pos}, nil
}

func isIdentStart(r rune) bool { return unicode.IsLetter(r) || r == '_' || r == '$' }
func isIdentPart(r rune) bool  { return isIdentStart(r) || unicode.IsDigit(r) }
