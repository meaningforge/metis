package expression

import (
	"strings"
	"testing"
)

func TestLexerNextTokenizesCoreForms(t *testing.T) {
	lexer := NewLexer(" orders.amount >= 10 AND status != 'paid' ")
	want := []struct {
		kind TokenKind
		text string
	}{
		{TokenIdent, "orders"},
		{TokenDot, "."},
		{TokenIdent, "amount"},
		{TokenGTE, ">="},
		{TokenNumber, "10"},
		{TokenIdent, "AND"},
		{TokenIdent, "status"},
		{TokenNEQ, "!="},
		{TokenString, "'paid'"},
		{TokenEOF, ""},
	}

	for i, expected := range want {
		tok, err := lexer.Next()
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		if tok.Kind != expected.kind || tok.Text != expected.text {
			t.Fatalf("token %d = {%q %q}, want {%q %q}", i, tok.Kind, tok.Text, expected.kind, expected.text)
		}
		if tok.End < tok.Pos {
			t.Fatalf("token %d has invalid span [%d,%d)", i, tok.Pos, tok.End)
		}
	}
}

func TestLexerNextPreservesByteSpansAcrossWhitespaceAndUTF8(t *testing.T) {
	input := "  café >= 2"
	lexer := NewLexer(input)

	ident, err := lexer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ident.Kind != TokenIdent || ident.Text != "café" {
		t.Fatalf("ident = %#v", ident)
	}
	if got := input[ident.Pos:ident.End]; got != "café" {
		t.Fatalf("ident span slices %q, want café", got)
	}

	gte, err := lexer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if got := input[gte.Pos:gte.End]; got != ">=" {
		t.Fatalf("operator span slices %q, want >=", got)
	}
}

func TestLexerNextHandlesQuotedIdentifiersAndEscapedStrings(t *testing.T) {
	lexer := NewLexer("\"order total\" 'it''s paid'")

	ident, err := lexer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ident.Kind != TokenIdent || ident.Text != "order total" {
		t.Fatalf("quoted ident = %#v", ident)
	}

	str, err := lexer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if str.Kind != TokenString || str.Text != "'it''s paid'" {
		t.Fatalf("string = %#v", str)
	}
}

func TestLexerNextRecognizesMultiCharacterOperators(t *testing.T) {
	for _, tc := range []struct {
		input string
		kind  TokenKind
	}{
		{"::", TokenDColon},
		{"->", TokenArrow},
		{"!=", TokenNEQ},
		{"<>", TokenNEQ},
		{"<=", TokenLTE},
		{">=", TokenGTE},
	} {
		t.Run(tc.input, func(t *testing.T) {
			tok, err := NewLexer(tc.input).Next()
			if err != nil {
				t.Fatal(err)
			}
			if tok.Kind != tc.kind || tok.Text != tc.input {
				t.Fatalf("token = %#v, want kind %q text %q", tok, tc.kind, tc.input)
			}
		})
	}
}

func TestLexerNextReportsLocalLexicalErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"unterminated string", "'paid", "unterminated string at 0"},
		{"unterminated quoted identifier", "\"orders", "unterminated quoted identifier at 0"},
		{"unexpected character", "@", "unexpected character '@' at 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLexer(tc.input).Next()
			if err == nil {
				t.Fatalf("Next(%q) unexpectedly succeeded", tc.input)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want containing %q", err, tc.want)
			}
		})
	}
}
