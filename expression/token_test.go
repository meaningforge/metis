package expression

import "testing"

func TestTokenKeywordMatchesIdentifiersCaseInsensitively(t *testing.T) {
	tok := Token{Kind: TokenIdent, Text: "AnD"}
	if !tok.Keyword("and") {
		t.Fatal("Keyword(and) = false, want true")
	}
	if tok.Keyword("or") {
		t.Fatal("Keyword(or) = true, want false")
	}
}

func TestTokenKeywordRejectsNonIdentifiers(t *testing.T) {
	tok := Token{Kind: TokenString, Text: "AND"}
	if tok.Keyword("AND") {
		t.Fatal("Keyword on non-identifier token = true, want false")
	}
}

func TestTokenSpanReturnsByteOffsets(t *testing.T) {
	tok := Token{Kind: TokenIdent, Text: "café", Pos: 2, End: 7}
	if got, want := tok.Span(), (SourceSpan{Start: 2, End: 7}); got != want {
		t.Fatalf("Span() = %#v, want %#v", got, want)
	}
}
