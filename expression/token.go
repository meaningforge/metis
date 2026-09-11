package expression

import "strings"

type TokenKind string

const (
	TokenEOF      TokenKind = "EOF"
	TokenIdent    TokenKind = "IDENT"
	TokenNumber   TokenKind = "NUMBER"
	TokenString   TokenKind = "STRING"
	TokenLParen   TokenKind = "("
	TokenRParen   TokenKind = ")"
	TokenLBracket TokenKind = "["
	TokenRBracket TokenKind = "]"
	TokenComma    TokenKind = ","
	TokenDot      TokenKind = "."
	TokenColon    TokenKind = ":"
	TokenDColon   TokenKind = "::"
	TokenArrow    TokenKind = "->"
	TokenQuestion TokenKind = "?"
	TokenPlus     TokenKind = "+"
	TokenMinus    TokenKind = "-"
	TokenStar     TokenKind = "*"
	TokenSlash    TokenKind = "/"
	TokenPercent  TokenKind = "%"
	TokenEQ       TokenKind = "="
	TokenNEQ      TokenKind = "!="
	TokenLT       TokenKind = "<"
	TokenLTE      TokenKind = "<="
	TokenGT       TokenKind = ">"
	TokenGTE      TokenKind = ">="
)

type Token struct {
	Kind TokenKind
	Text string
	Pos  int
	End  int
}

func (t Token) Keyword(word string) bool {
	return t.Kind == TokenIdent && strings.EqualFold(t.Text, word)
}

func (t Token) Span() SourceSpan { return SourceSpan{Start: t.Pos, End: t.End} }
