// Package validation checks selected queries against an explicitly configured
// database, using the production compiler and bounded planning capabilities.
package validation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/query"
	"go.yaml.in/yaml/v3"
)

const MaxInputBytes = 1 << 20

type Inventory struct {
	SchemaVersion int    `json:"schema_version"`
	Queries       []Case `json:"queries"`
}
type Case struct {
	ID    string              `json:"id"`
	Query query.SemanticQuery `json:"query"`
}

func LoadInventory(path, project string) (Inventory, error) {
	file, err := os.Open(path)
	if err != nil {
		return Inventory{}, fmt.Errorf("cannot read query inventory")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if err != nil {
		return Inventory{}, fmt.Errorf("cannot read query inventory")
	}
	return ParseInventory(data, project)
}

func ParseInventory(data []byte, project string) (Inventory, error) {
	var out Inventory
	if len(data) > MaxInputBytes || !json.Valid(data) {
		return out, fmt.Errorf("inventory requires bounded JSON")
	}
	var document yaml.Node
	if yaml.NewDecoder(bytes.NewReader(data)).Decode(&document) != nil || len(document.Content) != 1 {
		return out, fmt.Errorf("invalid inventory")
	}
	if err := strictShape(document.Content[0], reflect.TypeOf(out)); err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Inventory{}, fmt.Errorf("invalid semantic query inventory")
	}
	if out.SchemaVersion != 1 || len(out.Queries) == 0 || len(out.Queries) > 100 {
		return Inventory{}, fmt.Errorf("inventory requires version 1 and 1 through 100 queries")
	}
	seen := map[string]bool{}
	for _, c := range out.Queries {
		if c.ID == "" || len(c.ID) > 128 || strings.TrimSpace(c.ID) != c.ID || strings.ContainsAny(c.ID, "\r\n\x00") || seen[c.ID] || c.Query.Project != project || c.Query.Model == "" {
			return Inventory{}, fmt.Errorf("queries require unique bounded IDs and matching project")
		}
		seen[c.ID] = true
	}
	return out, nil
}

// Inspect raw nodes before shared custom unmarshallers can discard unknown keys.
func strictShape(n *yaml.Node, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	// Predicate is a tagged recursive sum with its own strict JSON decoder.
	// Traverse it as raw structure here so duplicate keys and numeric precision
	// remain visible, then let query.Predicate enforce its closed grammar.
	if typ == reflect.TypeOf(query.Predicate{}) {
		typ = reflect.TypeOf((*any)(nil)).Elem()
	}
	if n.Tag == "!!null" {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		if typ.Kind() != reflect.Struct && typ.Kind() != reflect.Interface {
			return fmt.Errorf("invalid inventory field type")
		}
		fields := map[string]reflect.Type{}
		if typ.Kind() == reflect.Struct {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				name := strings.Split(f.Tag.Get("json"), ",")[0]
				if name != "" && name != "-" {
					fields[name] = f.Type
				}
			}
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if seen[key] {
				return fmt.Errorf("duplicate inventory key")
			}
			seen[key] = true
			childType, ok := fields[key]
			if typ.Kind() == reflect.Interface {
				childType = typ
				ok = true
			}
			if !ok {
				return fmt.Errorf("unknown inventory field")
			}
			if err := strictShape(n.Content[i+1], childType); err != nil {
				return err
			}
		}
	} else if n.Kind == yaml.SequenceNode {
		childType := typ
		if typ.Kind() == reflect.Slice {
			childType = typ.Elem()
		} else if typ.Kind() != reflect.Interface {
			return fmt.Errorf("invalid inventory field type")
		}
		for _, child := range n.Content {
			if err := strictShape(child, childType); err != nil {
				return err
			}
		}
	} else if typ.Kind() == reflect.Interface && (n.Tag == "!!int" || n.Tag == "!!float") {
		// Filter's public decoder stores numeric values in float64.
		original, ok := new(big.Rat).SetString(n.Value)
		value, err := strconv.ParseFloat(n.Value, 64)
		roundtrip, valid := new(big.Rat).SetString(strconv.FormatFloat(value, 'g', -1, 64))
		if !ok || !valid || err != nil || original.Cmp(roundtrip) != 0 {
			return fmt.Errorf("filter numeric value loses precision")
		}
	}
	return nil
}
