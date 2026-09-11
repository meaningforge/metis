package command

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/workload"
	"github.com/meaningforge/metis/ossie"
)

// okfSemanticContext is a read-only projection of the generated Ossie model.
// It is the only authority for relationships and metrics in generated OKF.
type okfSemanticContext struct {
	Model ossie.SemanticModel
}

func loadOKFSemanticContext(root string, bundle workload.Bundle) (*okfSemanticContext, error) {
	if len(bundle.Models) != 1 {
		return nil, fmt.Errorf("OKF generation requires exactly one Ossie model")
	}
	document, err := ossie.NewLoader().LoadFile(filepath.Join(root, bundle.Models[0].Path))
	if err != nil {
		return nil, fmt.Errorf("load generated Ossie model: %w", err)
	}
	for _, model := range document.SemanticModel {
		if model.Name == bundle.Models[0].Model {
			return &okfSemanticContext{Model: model}, nil
		}
	}
	return nil, fmt.Errorf("generated Ossie model %q is absent", bundle.Models[0].Model)
}

func (s *okfSemanticContext) forConcept(concept referenceAgentConcept) (json.RawMessage, error) {
	datasets := append([]ossie.Dataset(nil), s.Model.Datasets...)
	relationships := append([]ossie.Relationship(nil), s.Model.Relationships...)
	metrics := append([]ossie.Metric(nil), s.Model.Metrics...)
	if strings.HasPrefix(concept.ID, "tables/") {
		table := strings.TrimPrefix(concept.ID, "tables/")
		datasets = nil
		for _, dataset := range s.Model.Datasets {
			if strings.EqualFold(lastSourceSegment(dataset.Source), table) {
				datasets = append(datasets, dataset)
			}
		}
		if len(datasets) == 0 {
			return nil, fmt.Errorf("Ossie model has no dataset for physical table %q", table)
		}
		name := datasets[0].Name
		relationships = filterRelationships(s.Model.Relationships, name)
		metrics = filterMetrics(s.Model.Metrics, name)
	}
	return json.Marshal(struct {
		Model         string               `json:"model"`
		Description   string               `json:"description,omitempty"`
		Datasets      []ossie.Dataset      `json:"datasets"`
		Relationships []ossie.Relationship `json:"relationships"`
		Metrics       []ossie.Metric       `json:"metrics"`
	}{s.Model.Name, s.Model.Description, datasets, relationships, metrics})
}

func lastSourceSegment(source string) string {
	parts := strings.Split(source, ".")
	return parts[len(parts)-1]
}

func filterRelationships(values []ossie.Relationship, dataset string) []ossie.Relationship {
	var result []ossie.Relationship
	for _, relationship := range values {
		if relationship.From == dataset || relationship.To == dataset {
			result = append(result, relationship)
		}
	}
	return result
}

func filterMetrics(values []ossie.Metric, dataset string) []ossie.Metric {
	var result []ossie.Metric
	for _, metric := range values {
		for _, dialect := range metric.Expression.Dialects {
			if strings.Contains(dialect.Expression, dataset+".") {
				result = append(result, metric)
				break
			}
		}
	}
	return result
}
