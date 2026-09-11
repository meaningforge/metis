package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
)

// OntologyDiagnostic is author-only evidence. Location is an Ossie document
// path, not a physical query or a runtime diagnostic.
type OntologyDiagnostic struct {
	Code     string
	Concept  string
	Location string
	Blocking bool
}

type OntologyTarget struct{ Model, Dataset, Field, Concept string }

// OntologyResolutionIndex is a derived, generation-owned read model. Accessors
// return copies; callers cannot modify graph or binding authority.
type OntologyResolutionIndex struct {
	concepts    map[string]*ontologyNode
	targets     []OntologyTarget
	diagnostics []OntologyDiagnostic
	ready       bool
	generation  string
}
type ontologyNode struct {
	name, kind, root string
	parents          []string
	raw              map[string]any
}

var ontologyBuiltins = []string{"Any", "Boolean", "Date", "DateTime", "Decimal", "Float", "Integer", "String"}

func (i *OntologyResolutionIndex) Ready() bool { return i != nil && i.ready }
func (i *OntologyResolutionIndex) GenerationIdentity() string {
	if i == nil {
		return ""
	}
	return i.generation
}
func (i *OntologyResolutionIndex) Diagnostics() []OntologyDiagnostic {
	if i == nil {
		return nil
	}
	return append([]OntologyDiagnostic(nil), i.diagnostics...)
}

// Targets returns direct and descendant mappings, never upward evidence for a subtype.
func (i *OntologyResolutionIndex) Targets(concept string) ([]OntologyTarget, bool) {
	if !i.Ready() {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(concept))
	if i.concepts[key] == nil {
		return nil, false
	}
	out := []OntologyTarget{}
	for _, target := range i.targets {
		if i.descends(strings.ToLower(target.Concept), key, map[string]bool{}) {
			out = append(out, target)
		}
	}
	return out, true
}
func (i *OntologyResolutionIndex) descends(child, parent string, seen map[string]bool) bool {
	if child == parent {
		return true
	}
	if seen[child] {
		return false
	}
	seen[child] = true
	node := i.concepts[child]
	if node == nil {
		return false
	}
	for _, next := range node.parents {
		if i.descends(next, parent, seen) {
			return true
		}
	}
	return false
}

func buildOntologyResolution(doc *ossie.Document, models map[string]*ModelIndex) *OntologyResolutionIndex {
	i := &OntologyResolutionIndex{concepts: map[string]*ontologyNode{}, ready: true}
	diagnostic := func(code, concept, location string, blocking bool) {
		if blocking {
			i.ready = false
		}
		if len(i.diagnostics) < 256 {
			i.diagnostics = append(i.diagnostics, OntologyDiagnostic{code, concept, location, blocking})
		}
	}
	if len(doc.Ontology) > 4096 || len(doc.OntologyMappings) > 4096 {
		diagnostic("ONTOLOGY_GRAPH_INVALID", "", "ontology", true)
		return i
	}
	for _, name := range ontologyBuiltins {
		kind := "ValueType"
		if name == "Any" {
			kind = "EntityType"
		}
		i.concepts[strings.ToLower(name)] = &ontologyNode{name: name, kind: kind, root: name}
	}
	for n, raw := range doc.Ontology {
		name := stringValue(raw["concept"])
		key := strings.ToLower(name)
		if name == "" || len(name) > 1024 || i.concepts[key] != nil {
			diagnostic("ONTOLOGY_GRAPH_INVALID", name, fmt.Sprintf("ontology[%d]", n), true)
			continue
		}
		kind := stringValue(raw["type"])
		if kind != "ValueType" && kind != "EntityType" {
			diagnostic("ONTOLOGY_GRAPH_INVALID", name, fmt.Sprintf("ontology[%d]", n), true)
		}
		parents := stringSlice(raw["extends"])
		if !ontologyStringList(raw["extends"]) || len(parents) > 128 {
			diagnostic("ONTOLOGY_GRAPH_INVALID", name, fmt.Sprintf("ontology[%d]", n), true)
		}
		if kind == "EntityType" {
			parents = append(parents, "Any")
		}
		for j := range parents {
			parents[j] = strings.ToLower(strings.TrimSpace(parents[j]))
		}
		sort.Strings(parents)
		i.concepts[key] = &ontologyNode{name: name, kind: kind, parents: parents, raw: raw}
	}
	visiting := map[string]bool{}
	var root func(string, int) string
	root = func(key string, depth int) string {
		node := i.concepts[key]
		if node == nil || visiting[key] || depth > 128 {
			return ""
		}
		if node.root != "" {
			return node.root
		}
		visiting[key] = true
		defer delete(visiting, key)
		result := ""
		for _, parent := range node.parents {
			p := i.concepts[parent]
			if p == nil || p.kind != node.kind {
				return ""
			}
			r := root(parent, depth+1)
			if r == "" || (result != "" && result != r) {
				return ""
			}
			result = r
		}
		node.root = result
		return result
	}
	keys := make([]string, 0, len(i.concepts))
	for key := range i.concepts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if root(key, 0) == "" {
			diagnostic("ONTOLOGY_GRAPH_INVALID", i.concepts[key].name, "ontology", true)
		}
		node := i.concepts[key]
		relations, ok := ontologyObjects(node.raw["relationships"])
		if !ok {
			diagnostic("ONTOLOGY_GRAPH_INVALID", node.name, "ontology", true)
			continue
		}
		for _, relation := range relations {
			roles, ok := ontologyObjects(relation["roles"])
			if !ok {
				diagnostic("ONTOLOGY_GRAPH_INVALID", node.name, "ontology", true)
				continue
			}
			for _, role := range roles {
				if i.concepts[strings.ToLower(stringValue(role["concept"]))] == nil {
					diagnostic("ONTOLOGY_GRAPH_INVALID", node.name, "ontology", true)
				}
			}
		}
	}
	// Graph failures cannot produce a partial resolution snapshot.
	if !i.ready {
		return i
	}
	modelNames := make([]string, 0, len(models))
	for name := range models {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	type locatedMapping struct {
		value    map[string]any
		location string
	}
	var mappings []locatedMapping
	for n, group := range doc.OntologyMappings {
		location := fmt.Sprintf("ontology_mappings[%d]", n)
		entries, ok := ontologyObjects(group["concept_mappings"])
		if !ok || len(entries) == 0 {
			diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", "", location, true)
			continue
		}
		for j, entry := range entries {
			mappings = append(mappings, locatedMapping{entry, fmt.Sprintf("%s.concept_mappings[%d]", location, j)})
			if len(mappings) > 4096 {
				diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", "", location, true)
				return i
			}
		}
	}
	for _, entry := range mappings {
		mapping := entry.value
		concept := stringValue(mapping["concept"])
		location := entry.location
		if !i.mappingConceptsKnown(mapping, 0) {
			diagnostic("ONTOLOGY_GRAPH_INVALID", concept, location, true)
			continue
		}
		if i.concepts[strings.ToLower(concept)] == nil {
			diagnostic("ONTOLOGY_GRAPH_INVALID", concept, location, true)
			continue
		}
		if _, exists := mapping["link_mappings"]; exists {
			diagnostic("ONTOLOGY_MAPPING_RESOLUTION_UNSUPPORTED", concept, location, false)
		}
		objects, ok := ontologyObjects(mapping["object_mappings"])
		if !ok || (len(objects) == 0 && mapping["link_mappings"] == nil) {
			diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", concept, location, true)
			continue
		}
		for j, object := range objects {
			loc := fmt.Sprintf("%s.object_mappings[%d]", location, j)
			targetConcept := concept
			if own := stringValue(object["concept"]); own != "" {
				targetConcept = own
			}
			node := i.concepts[strings.ToLower(targetConcept)]
			if node == nil {
				diagnostic("ONTOLOGY_GRAPH_INVALID", targetConcept, loc, true)
				continue
			}
			if _, exists := object["referent_mappings"]; exists {
				if _, both := object["expression"]; both {
					diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", targetConcept, loc, true)
					continue
				}
				diagnostic("ONTOLOGY_MAPPING_RESOLUTION_UNSUPPORTED", targetConcept, loc, false)
				continue
			}
			source := stringValue(object["expression"])
			if len(source) > 4096 {
				diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", targetConcept, loc, true)
				continue
			}
			parsed, err := expression.Parse(source, expression.ANSI)
			if err != nil {
				diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", targetConcept, loc, true)
				continue
			}
			identifier, bare := parsed.(*expression.IdentifierExpr)
			if !bare || len(identifier.Parts) != 2 {
				diagnostic("ONTOLOGY_MAPPING_RESOLUTION_UNSUPPORTED", targetConcept, loc, false)
				continue
			}
			valueRoot := node.root
			if node.kind == "EntityType" {
				valueRoot = i.identifierRoot(node)
				if valueRoot == "" {
					diagnostic("ONTOLOGY_ENTITY_IDENTIFIER_UNSUPPORTED", targetConcept, loc, true)
					continue
				}
			}
			bound := false
			for _, model := range modelNames {
				handle := models[model].Fields[strings.Join(identifier.Parts, ".")]
				if handle == nil {
					continue
				}
				bound = true
				if handle.Field.Dimension == nil {
					diagnostic("ONTOLOGY_MAPPING_TARGET_NOT_QUERYABLE", targetConcept, loc, false)
					continue
				}
				datatype := string(handle.Field.Datatype)
				if datatype == "" || datatype == "Opaque" || datatype == "Time" {
					diagnostic("ONTOLOGY_MAPPING_TYPE_UNPROVEN", targetConcept, loc, true)
					continue
				}
				if datatype != valueRoot && !(valueRoot == "DateTime" && datatype == "DateTimeTz") {
					diagnostic("ONTOLOGY_MAPPING_TYPE_MISMATCH", targetConcept, loc, true)
					continue
				}
				i.targets = append(i.targets, OntologyTarget{model, handle.Dataset, handle.Field.Name, node.name})
				if len(i.targets) > 65536 {
					diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", targetConcept, loc, true)
					return i
				}
			}
			if !bound {
				diagnostic("ONTOLOGY_MAPPING_TARGET_INVALID", targetConcept, loc, true)
			}
		}
	}
	return i
}

func ontologyObjects(value any) ([]map[string]any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, true
	case []map[string]any:
		if len(v) > 4096 {
			return nil, false
		}
		return v, true
	case []any:
		if len(v) > 4096 {
			return nil, false
		}
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, m)
		}
		return out, true
	default:
		return nil, false
	}
}

func (i *OntologyResolutionIndex) identifierRoot(node *ontologyNode) string {
	if !ontologyStringList(node.raw["identify_by"]) {
		return ""
	}
	identifiers := stringSlice(node.raw["identify_by"])
	if len(identifiers) != 1 {
		return ""
	}
	relationships, ok := ontologyObjects(node.raw["relationships"])
	if !ok {
		return ""
	}
	var found map[string]any
	for _, relation := range relationships {
		if stringValue(relation["name"]) == identifiers[0] {
			if found != nil {
				return ""
			}
			found = relation
		}
	}
	if found == nil || stringValue(found["multiplicity"]) != "OneToOne" {
		return ""
	}
	roles, ok := ontologyObjects(found["roles"])
	if !ok || len(roles) != 1 {
		return ""
	}
	role := i.concepts[strings.ToLower(stringValue(roles[0]["concept"]))]
	if role == nil || role.kind != "ValueType" {
		return ""
	}
	return role.root
}

func ontologyStringList(value any) bool {
	switch list := value.(type) {
	case nil:
		return true
	case []string:
		for _, item := range list {
			if strings.TrimSpace(item) == "" {
				return false
			}
		}
		return true
	case []any:
		for _, item := range list {
			if stringValue(item) == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Check references only at structured Ossie mapping positions. Expressions and
// arbitrary extension maps are never recursively interpreted as concept owners.
func (i *OntologyResolutionIndex) mappingConceptsKnown(object map[string]any, depth int) bool {
	if depth > 128 {
		return false
	}
	if name := stringValue(object["concept"]); name != "" && i.concepts[strings.ToLower(name)] == nil {
		return false
	}
	for _, key := range []string{"object_mappings", "referent_mappings", "link_mappings", "children"} {
		values, ok := ontologyObjects(object[key])
		if !ok {
			return false
		}
		for _, value := range values {
			if !i.mappingConceptsKnown(value, depth+1) {
				return false
			}
		}
	}
	if value, exists := object["object_mapping"]; exists {
		child, ok := value.(map[string]any)
		if !ok || !i.mappingConceptsKnown(child, depth+1) {
			return false
		}
	}
	return true
}
