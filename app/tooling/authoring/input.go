// Package authoring generates reviewable Ossie candidates from explicit catalog
// evidence and author choices, using the normal source validation services.
package authoring

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/meaningforge/metis/serrors"
	"go.yaml.in/yaml/v3"
)

const SchemaVersion = 1
const MaxCatalogBytes = 10 << 20
const MaxMappingBytes = 1 << 20

type Finding struct {
	Code         string               `json:"code"`
	Location     string               `json:"location"`
	CallerAction serrors.CallerAction `json:"caller_action"`
	Message      string               `json:"message"`
	InvalidInput bool                 `json:"-"`
}

func (f *Finding) Error() string {
	return fmt.Sprintf("%s at %s: %s (%s)", f.Code, f.Location, f.Message, f.CallerAction)
}
func finding(code, location, message string) error {
	action := serrors.CallerActionChangeModel
	if code == "AUTHORING_UNSUPPORTED_BACKEND" {
		action = serrors.CallerActionChangeTarget
	}
	return &Finding{Code: code, Location: location, Message: message, CallerAction: action}
}
func invalid(location, message string) error {
	return &Finding{Code: "AUTHORING_INVALID_INPUT", Location: location, Message: message, CallerAction: serrors.CallerActionChangeModel, InvalidInput: true}
}

type Snapshot struct {
	SchemaVersion int          `json:"schema_version" yaml:"schema_version"`
	Project       string       `json:"project" yaml:"project"`
	DataSource    string       `json:"data_source" yaml:"data_source"`
	Backend       string       `json:"backend" yaml:"backend"`
	Relations     []Relation   `json:"relations" yaml:"relations"`
	Digest        string       `json:"digest" yaml:"digest"`
	Observation   *Observation `json:"observation,omitempty" yaml:"observation"`
}
type Observation struct {
	ObservedAt    string `json:"observed_at,omitempty" yaml:"observed_at"`
	ServerVersion string `json:"server_version,omitempty" yaml:"server_version"`
}
type Relation struct {
	ID              string   `json:"id" yaml:"id"`
	Parts           []string `json:"parts" yaml:"parts"`
	ResolvedParts   []string `json:"resolved_parts,omitempty" yaml:"resolved_parts"`
	Outcome         string   `json:"outcome" yaml:"outcome"`
	ColumnsComplete bool     `json:"columns_complete" yaml:"columns_complete"`
	Columns         []Column `json:"columns" yaml:"columns"`
	Keys            []Key    `json:"keys,omitempty" yaml:"keys"`
}
type Column struct {
	Name       string     `json:"name" yaml:"name"`
	NativeType NativeType `json:"native_type" yaml:"native_type"`
	Nullable   string     `json:"nullable" yaml:"nullable"`
}
type NativeType struct {
	Name      string  `json:"name" yaml:"name"`
	Precision *int    `json:"precision,omitempty" yaml:"precision"`
	Scale     *int    `json:"scale,omitempty" yaml:"scale"`
	Timezone  *string `json:"timezone,omitempty" yaml:"timezone"`
	Length    *int    `json:"length,omitempty" yaml:"length"`
}
type Key struct {
	Kind    string   `json:"kind" yaml:"kind"`
	Columns []string `json:"columns" yaml:"columns"`
}
type Mapping struct {
	SchemaVersion  int              `json:"schema_version" yaml:"schema_version"`
	Project        string           `json:"project" yaml:"project"`
	Model          string           `json:"model" yaml:"model"`
	DataSource     string           `json:"data_source,omitempty" yaml:"data_source"`
	Datasets       []DatasetMapping `json:"datasets" yaml:"datasets"`
	StarterMetrics []StarterMetric  `json:"starter_metrics,omitempty" yaml:"starter_metrics"`
}
type DatasetMapping struct {
	Relation string         `json:"relation" yaml:"relation"`
	Name     string         `json:"name" yaml:"name"`
	Fields   []FieldMapping `json:"fields" yaml:"fields"`
}
type FieldMapping struct {
	Column    string `json:"column" yaml:"column"`
	Name      string `json:"name" yaml:"name"`
	Dimension bool   `json:"dimension,omitempty" yaml:"dimension"`
}
type StarterMetric struct {
	Name    string `json:"name" yaml:"name"`
	Kind    string `json:"kind" yaml:"kind"`
	Dataset string `json:"dataset" yaml:"dataset"`
}

func LoadSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	data, err := readBounded(path, MaxCatalogBytes)
	if err != nil {
		return s, err
	}
	return ParseSnapshot(data)
}
func LoadMapping(path string) (Mapping, error) {
	var m Mapping
	data, err := readBounded(path, MaxMappingBytes)
	if err != nil {
		return m, err
	}
	return ParseMapping(data)
}
func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, invalid("input", "input must be a readable regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid("input", "cannot open input file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, invalid("input", "cannot read input file")
	}
	if int64(len(data)) > limit {
		return nil, invalid("input", "input exceeds size limit")
	}
	return data, nil
}
func ParseSnapshot(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := decodeStrict(data, &s, MaxCatalogBytes); err != nil {
		return s, err
	}
	if err := validateSnapshot(s); err != nil {
		return s, err
	}
	return s, nil
}
func ParseMapping(data []byte) (Mapping, error) {
	var m Mapping
	if err := decodeStrict(data, &m, MaxMappingBytes); err != nil {
		return m, err
	}
	if m.SchemaVersion != SchemaVersion {
		return m, invalid("schema_version", "unsupported mapping version")
	}
	return m, nil
}
func decodeStrict(data []byte, target any, limit int) error {
	if len(data) > limit {
		return invalid("input", "input exceeds size limit")
	}
	var root yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&root); err != nil {
		return invalid("input", "invalid YAML or JSON document")
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return invalid("input", "expected one mapping document")
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return invalid("input", "expected exactly one document")
	}
	var check func(*yaml.Node) error
	check = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return invalid("input", "YAML anchors and aliases are unsupported")
		}
		if n.Kind == yaml.ScalarNode {
			switch n.Tag {
			case "!!str", "!!int", "!!bool", "!!null":
			default:
				return invalid("input", "unsupported scalar tag")
			}
		}
		if n.Kind == yaml.MappingNode && n.Tag != "!!map" || n.Kind == yaml.SequenceNode && n.Tag != "!!seq" {
			return invalid("input", "unsupported collection tag")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return invalid("input", "duplicate or non-string key")
				}
				seen[k.Value] = true
			}
		}
		for _, child := range n.Content {
			if err := check(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(&root); err != nil {
		return err
	}
	if err := checkInputShape(root.Content[0], reflect.TypeOf(target).Elem(), "input"); err != nil {
		return err
	}
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(target); err != nil {
		return invalid("input", "unknown field or invalid field type")
	}
	return nil
}

// Find the offending typed path without echoing parser errors or input values.
func checkInputShape(node *yaml.Node, typ reflect.Type, path string) error {
	if typ.Kind() == reflect.Pointer {
		if node.Tag == "!!null" {
			return nil
		}
		return checkInputShape(node, typ.Elem(), path)
	}
	switch typ.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return invalid(path, "expected an object")
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = f.Type
			}
		}
		for i := 0; i < len(node.Content); i += 2 {
			name := node.Content[i].Value
			field, ok := fields[name]
			if !ok {
				return invalid(fmt.Sprintf("%s.entries[%d]", path, i/2), "unknown input field")
			}
			if err := checkInputShape(node.Content[i+1], field, path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Tag == "!!null" {
			return nil
		}
		if node.Kind != yaml.SequenceNode {
			return invalid(path, "expected an array")
		}
		for i, child := range node.Content {
			if err := checkInputShape(child, typ.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return invalid(path, "expected an explicit string")
		}
	case reflect.Bool:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			return invalid(path, "expected a boolean")
		}
	case reflect.Int:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			return invalid(path, "expected an integer")
		}
	default:
		return invalid(path, "unsupported input shape")
	}
	return nil
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// CatalogDigest normalizes unordered inventories, preserving identifier part
// order, case, backend key meaning, and column evidence. Observations are excluded.
func CatalogDigest(s Snapshot) (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	var stable Snapshot
	if err = json.Unmarshal(data, &stable); err != nil {
		return "", err
	}
	stable.Digest = ""
	stable.Observation = nil
	for i := range stable.Relations {
		r := &stable.Relations[i]
		sort.Slice(r.Columns, func(i, j int) bool { return r.Columns[i].Name < r.Columns[j].Name })
		sort.Slice(r.Keys, func(i, j int) bool {
			a, _ := json.Marshal(r.Keys[i])
			b, _ := json.Marshal(r.Keys[j])
			return string(a) < string(b)
		})
	}
	sort.Slice(stable.Relations, func(i, j int) bool { return stable.Relations[i].ID < stable.Relations[j].ID })
	data, err = json.Marshal(stable)
	return digest(data), err
}
func boundedName(value string) bool {
	return value != "" && len(value) <= 256 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
func validateSnapshot(s Snapshot) error {
	if s.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "unsupported catalog version")
	}
	if !boundedName(s.Project) || !boundedName(s.DataSource) {
		return invalid("catalog", "project and logical data_source are required")
	}
	if s.Backend != "doris" && s.Backend != "clickhouse" {
		return finding("AUTHORING_UNSUPPORTED_BACKEND", "backend", "offline mappings currently support Doris and ClickHouse")
	}
	if len(s.Relations) == 0 || len(s.Relations) > 200 {
		return invalid("relations", "catalog requires 1 through 200 relations")
	}
	seen, physical := map[string]bool{}, map[string]bool{}
	total := 0
	for i, r := range s.Relations {
		loc := fmt.Sprintf("relations[%d]", i)
		if !boundedName(r.ID) || seen[r.ID] {
			return invalid(loc, "selector IDs must be nonempty and unique")
		}
		seen[r.ID] = true
		if len(r.Parts) == 0 || len(r.Parts) > 3 {
			return invalid(loc+".parts", "expected 1 through 3 identifier parts")
		}
		for _, part := range r.Parts {
			if !boundedName(part) {
				return invalid(loc+".parts", "invalid identifier part")
			}
		}
		key, _ := json.Marshal(r.Parts)
		if physical[string(key)] {
			return invalid(loc, "duplicate requested physical relation")
		}
		physical[string(key)] = true
		if len(r.ResolvedParts) > 0 {
			if len(r.ResolvedParts) > 3 {
				return invalid(loc+".resolved_parts", "invalid resolved identity")
			}
			for _, part := range r.ResolvedParts {
				if !boundedName(part) {
					return invalid(loc+".resolved_parts", "invalid resolved identifier part")
				}
			}
		}
		if r.Outcome != "found" || !r.ColumnsComplete || len(r.Columns) == 0 {
			return finding("AUTHORING_CATALOG_INCOMPLETE", loc, "every relation must be found with a complete, nonempty column inventory")
		}
		total += len(r.Columns)
		if total > 10000 {
			return invalid("relations", "catalog exceeds 10000 columns")
		}
		columns := map[string]bool{}
		for j, c := range r.Columns {
			if !boundedName(c.Name) || columns[c.Name] || !boundedName(c.NativeType.Name) {
				return invalid(fmt.Sprintf("%s.columns[%d]", loc, j), "invalid or duplicate column identity/type")
			}
			columns[c.Name] = true
			if c.Nullable != "nullable" && c.Nullable != "not_null" && c.Nullable != "unknown" {
				return invalid(loc, "nullable must be nullable, not_null, or unknown")
			}
			if c.NativeType.Precision != nil && (*c.NativeType.Precision < 0 || *c.NativeType.Precision > 1000) || c.NativeType.Scale != nil && (*c.NativeType.Scale < 0 || *c.NativeType.Scale > 1000) {
				return invalid(loc, "invalid bounded native precision or scale")
			}
			if c.NativeType.Timezone != nil && !boundedName(*c.NativeType.Timezone) {
				return invalid(loc, "invalid timezone evidence")
			}
			if c.NativeType.Length != nil && (*c.NativeType.Length <= 0 || *c.NativeType.Length > MaxCatalogBytes) {
				return invalid(loc, "invalid native length evidence")
			}
		}
		for _, k := range r.Keys {
			if k.Kind != "primary" && k.Kind != "unique" && k.Kind != "sorting" && k.Kind != "duplicate" && k.Kind != "aggregate" {
				return invalid(loc+".keys", "unsupported observed key kind")
			}
			if len(k.Columns) == 0 {
				return invalid(loc+".keys", "key columns are required")
			}
			parts := map[string]bool{}
			for _, col := range k.Columns {
				if !columns[col] || parts[col] {
					return invalid(loc+".keys", "key must reference unique inventoried columns")
				}
				parts[col] = true
			}
		}
	}
	computed, err := CatalogDigest(s)
	if err != nil {
		return invalid("digest", "cannot hash catalog")
	}
	if s.Digest != computed {
		return finding("AUTHORING_CATALOG_DIGEST_MISMATCH", "digest", "catalog digest must match normalized stable evidence")
	}
	return nil
}
