package manifest

import (
	"sort"
	"strings"
)

// OntologyConceptIndex is discovery-only metadata derived from Apache Ossie
// concept declarations and implicit built-ins. Raw mapping text is excluded.
// It intentionally does not participate in
// Resolver, Planner, or compilation.
type OntologyConceptIndex struct {
	Name        string
	Type        string
	Description string
	Extends     []string
}

func buildOntologyIndex(ontology []map[string]any) map[string]*OntologyConceptIndex {
	out := map[string]*OntologyConceptIndex{}
	for _, raw := range ontology {
		name := stringValue(raw["concept"])
		if name == "" {
			continue
		}
		out[strings.ToLower(name)] = &OntologyConceptIndex{
			Name:        name,
			Type:        stringValue(raw["type"]),
			Description: stringValue(raw["description"]),
			Extends:     stringSlice(raw["extends"]),
		}
	}

	for _, name := range ontologyBuiltins {
		key := strings.ToLower(name)
		if out[key] == nil {
			kind := "ValueType"
			if name == "Any" {
				kind = "EntityType"
			}
			out[key] = &OntologyConceptIndex{Name: name, Type: kind}
		}
	}
	for _, idx := range out {
		sort.Strings(idx.Extends)
	}
	return out
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func stringSlice(value any) []string {
	var out []string
	switch v := value.(type) {
	case []string:
		out = append(out, v...)
	case []any:
		for _, item := range v {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
