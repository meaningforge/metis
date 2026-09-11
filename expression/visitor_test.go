package expression

import "testing"

type countingVisitor struct{ enters, leaves int }

func (v *countingVisitor) Enter(Expr) bool { v.enters++; return true }
func (v *countingVisitor) Leave(Expr)      { v.leaves++ }

func TestVisitorContractIsBalanced(t *testing.T) {
	ast, err := Parse("sum(a + b)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	v := &countingVisitor{}
	Walk(ast, v)
	if v.enters == 0 || v.enters != v.leaves {
		t.Fatalf("enter=%d leave=%d", v.enters, v.leaves)
	}
}
