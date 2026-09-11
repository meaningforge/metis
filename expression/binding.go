package expression

import (
	"fmt"
	"sort"
	"strings"
)

// ReferenceResolver binds all non-local references in one expression at once.
// Batch binding lets SemanticManifest apply expression-wide scope rules, such as
// requiring several unqualified fields to resolve to one semantic dataset.
type ReferenceResolver interface {
	ResolveReferences([]Reference) (map[Reference]BoundSymbol, error)
}

// BoundExpression is the stable output of semantic symbol binding. Expr keeps
// the source-spanned syntax tree while Bindings assigns every external
// reference a canonical semantic symbol.
type BoundExpression struct {
	Expr     Expr
	Bindings map[Reference]BoundSymbol
}

func Bind(expr Expr, resolver ReferenceResolver) (BoundExpression, error) {
	refs := CollectReferences(expr)
	bindings := map[Reference]BoundSymbol{}
	if len(refs) > 0 {
		if resolver == nil {
			return BoundExpression{}, fmt.Errorf("expression reference resolver is required")
		}
		var err error
		bindings, err = resolver.ResolveReferences(refs)
		if err != nil {
			return BoundExpression{}, err
		}
		for _, ref := range refs {
			if _, ok := bindings[ref]; !ok {
				name := ref.Name
				if ref.Qualifier != "" {
					name = strings.Join([]string{ref.Qualifier, ref.Name}, ".")
				}
				return BoundExpression{}, fmt.Errorf("expression reference %q was not bound", name)
			}
		}
	}
	owned := make(map[Reference]BoundSymbol, len(bindings))
	for ref, symbol := range bindings {
		owned[ref] = symbol
	}
	return BoundExpression{Expr: expr, Bindings: owned}, nil
}

// Resolve implements SymbolResolver so the type analyzer consumes the exact
// symbols established by Bind instead of resolving identifiers a second time.
func (b BoundExpression) Resolve(parts []string) (BoundSymbol, bool) {
	if len(parts) == 0 {
		return BoundSymbol{}, false
	}
	ref := Reference{Name: parts[0]}
	if len(parts) > 1 {
		ref = Reference{Qualifier: parts[0], Name: parts[1]}
	}
	symbol, ok := b.Bindings[ref]
	return symbol, ok
}

func (b BoundExpression) Symbols() []BoundSymbol {
	refs := make([]Reference, 0, len(b.Bindings))
	for ref := range b.Bindings {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Qualifier != refs[j].Qualifier {
			return refs[i].Qualifier < refs[j].Qualifier
		}
		return refs[i].Name < refs[j].Name
	})
	out := make([]BoundSymbol, 0, len(refs))
	for _, ref := range refs {
		out = append(out, b.Bindings[ref])
	}
	return out
}
