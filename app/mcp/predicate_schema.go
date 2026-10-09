package mcp

import "github.com/google/jsonschema-go/jsonschema"

func attachPredicateSchema(schema *jsonschema.Schema) {
	if schema == nil || schema.Properties == nil || schema.Properties["filters"] == nil {
		return
	}
	if schema.Defs == nil {
		schema.Defs = map[string]*jsonschema.Schema{}
	}
	falseSchema := func() *jsonschema.Schema { return &jsonschema.Schema{Not: &jsonschema.Schema{}} }
	ref := func() *jsonschema.Schema { return &jsonschema.Schema{Ref: "#/$defs/semantic_predicate"} }
	kind := func(value string) *jsonschema.Schema {
		constant := any(value)
		return &jsonschema.Schema{Type: "string", Const: &constant}
	}
	scalar := func() *jsonschema.Schema {
		return &jsonschema.Schema{Types: []string{"null", "string", "number", "boolean"}}
	}
	maxValues := 256
	value := &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		scalar(),
		{Type: "array", Items: scalar(), MaxItems: &maxValues},
	}}
	filter := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"field":    {Type: "string", Description: "Canonical metric or dimension ref returned by semantic discovery."},
			"operator": {Type: "string", Enum: []any{"eq", "neq", "gt", "gte", "lt", "lte", "in", "not_in", "between", "is_null", "is_not_null"}},
			"value":    value,
		},
		Required:             []string{"field", "operator"},
		AdditionalProperties: falseSchema(),
	}
	leaf := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"kind":   kind("filter"),
			"filter": filter,
		},
		Required:             []string{"kind", "filter"},
		AdditionalProperties: falseSchema(),
	}
	andOr := func(value string) *jsonschema.Schema {
		minChildren, maxChildren := 2, 32
		return &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"kind":     kind(value),
				"children": {Type: "array", Items: ref(), MinItems: &minChildren, MaxItems: &maxChildren},
			},
			Required:             []string{"kind", "children"},
			AdditionalProperties: falseSchema(),
		}
	}
	not := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"kind":  kind("not"),
			"child": ref(),
		},
		Required:             []string{"kind", "child"},
		AdditionalProperties: falseSchema(),
	}
	exists := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"kind":         kind("exists"),
			"relationship": {Type: "string", Description: "Declared one-hop relationship from the source population to the detail dataset."},
			"where":        ref(),
		},
		Required:             []string{"kind", "relationship", "where"},
		AdditionalProperties: falseSchema(),
	}
	schema.Defs["semantic_predicate"] = &jsonschema.Schema{OneOf: []*jsonschema.Schema{leaf, andOr("and"), andOr("or"), not, exists}}
	schema.Properties["filters"] = &jsonschema.Schema{
		Ref:         "#/$defs/semantic_predicate",
		Description: "Typed semantic predicates as one tagged filter, and, or, not, or relationship exists object. Leaf operators include eq, neq, gt, gte, lt, lte, in, not_in, between, is_null, and is_not_null; between is inclusive; date/time values use ISO-8601. Root conjunctions are combined with AND. A relationship exists predicate is allowed only as the root or a direct child of the root AND and filters the source population by matching detail rows.",
	}
}
