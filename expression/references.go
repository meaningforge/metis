package expression

import "strings"

type Reference struct {
	Qualifier string
	Name      string
}

type ReferenceCollector struct {
	refs   []Reference
	scopes []map[string]struct{}
}

func CollectReferences(expr Expr) []Reference {
	c := &ReferenceCollector{}
	Walk(expr, c)
	seen := map[Reference]struct{}{}
	out := make([]Reference, 0, len(c.refs))
	for _, ref := range c.refs {
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (c *ReferenceCollector) local(name string) bool {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if _, ok := c.scopes[i][strings.ToLower(name)]; ok {
			return true
		}
	}
	return false
}

func (c *ReferenceCollector) Enter(expr Expr) bool {
	switch e := expr.(type) {
	case *IdentifierExpr:
		if len(e.Parts) == 0 || c.local(e.Parts[0]) {
			return false
		}
		if len(e.Parts) == 1 {
			c.refs = append(c.refs, Reference{Name: e.Parts[0]})
		} else {
			c.refs = append(c.refs, Reference{Qualifier: e.Parts[0], Name: e.Parts[1]})
		}
		return false
	case *LiteralExpr, *WildcardExpr:
		return false
	case *LambdaExpr:
		scope := map[string]struct{}{}
		for _, p := range e.Params {
			scope[strings.ToLower(p)] = struct{}{}
		}
		c.scopes = append(c.scopes, scope)
	}
	return true
}

func (c *ReferenceCollector) Leave(expr Expr) {
	if _, ok := expr.(*LambdaExpr); ok {
		c.scopes = c.scopes[:len(c.scopes)-1]
	}
}
