package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"go.yaml.in/yaml/v3"
)

type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeRemoved  ChangeType = "removed"
	ChangeModified ChangeType = "modified"
)

type SemanticChange struct {
	Type         ChangeType `json:"type"`
	AssetKind    string     `json:"asset_kind"`
	Reference    string     `json:"reference"`
	BeforeDigest string     `json:"before_digest,omitempty"`
	AfterDigest  string     `json:"after_digest,omitempty"`
}

type SemanticDiff struct {
	SchemaVersion   int              `json:"schema_version"`
	ProjectID       string           `json:"project_id"`
	BaseDigest      string           `json:"base_digest"`
	CandidateDigest string           `json:"candidate_digest"`
	Changes         []SemanticChange `json:"changes"`
}

type indexedAsset struct {
	kind   string
	digest string
}

func Compare(base, candidate *ProjectSource) (SemanticDiff, error) {
	if base == nil || candidate == nil || base.Document == nil || candidate.Document == nil {
		return SemanticDiff{}, fmt.Errorf("two validated semantic candidates are required")
	}
	if base.Bundle.ProjectID != candidate.Bundle.ProjectID {
		return SemanticDiff{}, fmt.Errorf("semantic diff requires one project: %q != %q", base.Bundle.ProjectID, candidate.Bundle.ProjectID)
	}
	left, err := indexCandidate(base)
	if err != nil {
		return SemanticDiff{}, err
	}
	right, err := indexCandidate(candidate)
	if err != nil {
		return SemanticDiff{}, err
	}
	refs := make(map[string]struct{}, len(left)+len(right))
	for ref := range left {
		refs[ref] = struct{}{}
	}
	for ref := range right {
		refs[ref] = struct{}{}
	}
	ordered := make([]string, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)
	changes := make([]SemanticChange, 0)
	for _, ref := range ordered {
		before, beforeOK := left[ref]
		after, afterOK := right[ref]
		switch {
		case !beforeOK:
			changes = append(changes, SemanticChange{Type: ChangeAdded, AssetKind: after.kind, Reference: ref, AfterDigest: after.digest})
		case !afterOK:
			changes = append(changes, SemanticChange{Type: ChangeRemoved, AssetKind: before.kind, Reference: ref, BeforeDigest: before.digest})
		case before.digest != after.digest:
			changes = append(changes, SemanticChange{Type: ChangeModified, AssetKind: after.kind, Reference: ref, BeforeDigest: before.digest, AfterDigest: after.digest})
		}
	}
	return SemanticDiff{
		SchemaVersion: DiffSchemaVersion, ProjectID: base.Bundle.ProjectID,
		BaseDigest: base.Bundle.ContentDigest, CandidateDigest: candidate.Bundle.ContentDigest,
		Changes: changes,
	}, nil
}

func indexCandidate(candidate *ProjectSource) (map[string]indexedAsset, error) {
	assets := make(map[string]indexedAsset)
	for _, document := range candidate.Bundle.Documents {
		ref := strings.Join([]string{"document", document.Source, document.Path}, "/")
		assets[ref] = indexedAsset{kind: "document", digest: document.Digest}
	}
	for _, model := range candidate.Document.SemanticModel {
		modelRef := "model/" + model.Name
		if err := addAsset(assets, "model", modelRef, model); err != nil {
			return nil, err
		}
		for _, dataset := range model.Datasets {
			datasetRef := modelRef + "/dataset/" + dataset.Name
			if err := addAsset(assets, "dataset", datasetRef, dataset); err != nil {
				return nil, err
			}
			for _, field := range dataset.Fields {
				if err := addAsset(assets, "field", datasetRef+"/field/"+field.Name, field); err != nil {
					return nil, err
				}
			}
		}
		for _, metric := range model.Metrics {
			if err := addAsset(assets, "metric", modelRef+"/metric/"+metric.Name, metric); err != nil {
				return nil, err
			}
		}
		for _, relationship := range model.Relationships {
			if err := addAsset(assets, "relationship", modelRef+"/relationship/"+relationship.Name, relationship); err != nil {
				return nil, err
			}
		}
	}
	for index, concept := range candidate.Document.Ontology {
		name, _ := concept["concept"].(string)
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("index-%04d", index)
		}
		if err := addAsset(assets, "ontology_concept", "ontology/"+name, concept); err != nil {
			return nil, err
		}
	}
	for _, mapping := range candidate.Document.OntologyMappings {
		name, _ := mapping["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			body, err := json.Marshal(mapping)
			if err != nil {
				return nil, fmt.Errorf("canonicalize anonymous ontology mapping: %w", err)
			}
			name = strings.TrimPrefix(digestBytes(body), "sha256:")
		}
		if err := addAsset(assets, "ontology_mapping", "ontology_mapping/"+name, mapping); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func addAsset(index map[string]indexedAsset, kind, ref string, value any) error {
	if _, exists := index[ref]; exists {
		return fmt.Errorf("duplicate canonical semantic reference %q", ref)
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("canonicalize %s %q: %w", kind, ref, err)
	}
	index[ref] = indexedAsset{kind: kind, digest: digestBytes(body)}
	return nil
}

func FormatDocument(body []byte) ([]byte, error) {
	document, err := ossie.NewLoader().Load(body)
	if err != nil {
		return nil, err
	}
	formatted, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	return append(bytes.TrimRight(formatted, "\n"), '\n'), nil
}
