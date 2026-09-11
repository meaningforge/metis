package mcp

import (
	"encoding/json"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

var analyticalMemberValueSchema = &jsonschema.Schema{
	Types: []string{"null", "string", "number", "boolean"},
}

// analyticalOutputSchema teaches schema reflection that json.RawMessage is a
// validated analytical member scalar rather than its Go representation
// ([]byte). If a future result type cannot be reflected, retain a permissive
// object schema so one analytical tool cannot prevent the MCP server starting;
// the detailed schema contract remains covered by unit tests.
func analyticalOutputSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[json.RawMessage](): analyticalMemberValueSchema,
		},
	})
	if err != nil {
		return &jsonschema.Schema{Types: []string{"object"}}
	}
	return schema
}
