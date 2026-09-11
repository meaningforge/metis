package expression

import "strings"

// TypeRef is the parser-level representation of a SQL type reference. It is
// intentionally syntactic: SemanticManifest/type validation may attach meaning later.
type TypeRef struct {
	Name   string
	Args   []TypeArg
	Source SourceSpan
}

type TypeArg struct {
	Type  *TypeRef
	Value string
}

func TypeName(name string) TypeRef { return TypeRef{Name: name} }

func (t TypeRef) Span() SourceSpan { return t.Source }

func (t TypeRef) String() string {
	if len(t.Args) == 0 {
		return t.Name
	}
	parts := make([]string, 0, len(t.Args))
	for _, arg := range t.Args {
		if arg.Type != nil {
			parts = append(parts, arg.Type.String())
		} else {
			parts = append(parts, arg.Value)
		}
	}
	return t.Name + "(" + strings.Join(parts, ",") + ")"
}
