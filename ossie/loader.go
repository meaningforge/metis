package ossie

import (
	"bytes"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

type Loader struct{}

func NewLoader() *Loader { return &Loader{} }

func (l *Loader) LoadFile(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ossie model: %w", err)
	}
	return l.Load(b)
}

// Load decodes YAML or JSON into the schema-derived Ossie binding and then
// applies Metis semantic validation.
//
// KnownFields is intentionally enabled: the official Ossie schema uses
// additionalProperties=false for semantic objects. The one extensible object,
// AIContext, is represented as any and therefore retains arbitrary properties.
func (l *Loader) Load(data []byte) (*Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode ossie document: %w", err)
	}
	if err := ValidateDocument(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}
