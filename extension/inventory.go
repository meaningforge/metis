package extension

import (
	"errors"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/ossie"
)

// Location identifies the Ossie owner of a preserved custom extension. It is
// runtime observation metadata only and is not a second serialized schema.
type Location struct {
	Scope string
	Owner string
}

// Interpreter is an explicit vendor contract that may classify a preserved
// Ossie custom extension as requiring semantic capabilities. Unknown vendors
// are not guessed to be critical and therefore remain opaque-preserved.
type Interpreter interface {
	Vendor() ossie.Vendor
	Requirements(Location, ossie.CustomExtension) ([]Requirement, error)
}

var ErrDuplicateInterpreter = errors.New("duplicate extension interpreter")

// Inventory derives capability requirements from explicit extension-aware
// interpreters while leaving unknown extension payloads untouched.
type Inventory struct {
	interpreters map[ossie.Vendor]Interpreter
}

func NewInventory() *Inventory {
	inventory := &Inventory{interpreters: make(map[ossie.Vendor]Interpreter)}
	// Built-in semantic-critical contracts are explicit interpreters, not name
	// heuristics. Their capability still has to be registered for the selected
	// target before compilation can proceed.
	_ = inventory.Register(MetricScaleInterpreter{})
	return inventory
}

func (i *Inventory) Register(interpreter Interpreter) error {
	if i == nil {
		return errors.New("extension capability inventory is nil")
	}
	if interpreter == nil || interpreter.Vendor() == "" {
		return errors.New("extension interpreter vendor is required")
	}
	vendor := interpreter.Vendor()
	if _, ok := i.interpreters[vendor]; ok {
		return fmt.Errorf("%w: vendor=%s", ErrDuplicateInterpreter, vendor)
	}
	i.interpreters[vendor] = interpreter
	return nil
}

func (i *Inventory) RequirementsForModel(model *ossie.SemanticModel) ([]Requirement, error) {
	if model == nil || i == nil {
		return nil, nil
	}

	var requirements []Requirement
	observe := func(location Location, extensions []ossie.CustomExtension) error {
		for _, customExtension := range extensions {
			interpreter, ok := i.interpreters[customExtension.VendorName]
			if !ok {
				continue
			}
			observed, err := interpreter.Requirements(location, customExtension)
			if err != nil {
				return fmt.Errorf("interpret extension vendor=%s scope=%s owner=%s: %w", customExtension.VendorName, location.Scope, location.Owner, err)
			}
			requirements = append(requirements, observed...)
		}
		return nil
	}

	if err := observe(Location{Scope: "semantic_model", Owner: model.Name}, model.CustomExtensions); err != nil {
		return nil, err
	}
	for di := range model.Datasets {
		dataset := &model.Datasets[di]
		if err := observe(Location{Scope: "dataset", Owner: dataset.Name}, dataset.CustomExtensions); err != nil {
			return nil, err
		}
		for fi := range dataset.Fields {
			field := &dataset.Fields[fi]
			if err := observe(Location{Scope: "field", Owner: dataset.Name + "." + field.Name}, field.CustomExtensions); err != nil {
				return nil, err
			}
		}
	}
	for mi := range model.Metrics {
		metric := &model.Metrics[mi]
		if err := observe(Location{Scope: "metric", Owner: metric.Name}, metric.CustomExtensions); err != nil {
			return nil, err
		}
	}
	for ri := range model.Relationships {
		relationship := &model.Relationships[ri]
		if err := observe(Location{Scope: "relationship", Owner: relationship.Name}, relationship.CustomExtensions); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(requirements, func(a, b int) bool {
		left := requirementKey(requirements[a])
		right := requirementKey(requirements[b])
		return left < right
	})
	return requirements, nil
}

func requirementKey(req Requirement) string {
	return req.Identity.String() + "\x00" + req.Capability + "\x00" + req.Version
}
