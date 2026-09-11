package source

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const (
	DiagnosticProjectConfig     DiagnosticCode = "PROJECT_CONFIG_INVALID"
	DiagnosticSourceResolution  DiagnosticCode = "SEMANTIC_SOURCE_RESOLUTION_FAILED"
	DiagnosticDocumentInvalid   DiagnosticCode = "OSSIE_DOCUMENT_INVALID"
	DiagnosticAssemblyInvalid   DiagnosticCode = "SEMANTIC_PROJECT_ASSEMBLY_INVALID"
	DiagnosticManifestInvalid   DiagnosticCode = "SEMANTIC_MANIFEST_INVALID"
	DiagnosticQualityEvaluation DiagnosticCode = "MODEL_QUALITY_EVALUATION_FAILED"
)

func LoadProject(projectID, configPath string) (*ProjectSource, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, &LoadError{Code: DiagnosticProjectConfig, Err: fmt.Errorf("project ID is required")}
	}
	absoluteConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticProjectConfig, Location: configPath, Err: err}
	}
	config, err := execution.LoadProjectConfigFile(absoluteConfig)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticProjectConfig, Location: absoluteConfig, Err: err}
	}
	baseDir := filepath.Dir(absoluteConfig)
	var documents []SourceDocument
	locations := map[string]string{}
	for _, key := range sortedKeys(config.SemanticSources) {
		source := config.SemanticSources[key]
		paths, err := expandSemanticSourcePaths(baseDir, source)
		if err != nil {
			return nil, &LoadError{Code: DiagnosticSourceResolution, Location: key, Err: err}
		}
		for _, path := range paths {
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, &LoadError{Code: DiagnosticSourceResolution, Location: path, Err: err}
			}
			identity := sourceIdentityPath(baseDir, path)
			locations[identity] = path
			documents = append(documents, SourceDocument{Source: key, Path: identity, Content: body})
		}
	}
	candidate, err := loadProjectDocuments(projectID, config, documents, locations)
	if failure, ok := err.(*LoadError); ok && failure.Code == DiagnosticQualityEvaluation {
		failure.Location = absoluteConfig
	}
	return candidate, err
}

// LoadProjectDocuments is the shared assembly boundary for local files and
// verified managed source bytes. It owns Ossie loading, merging, manifest and
// quality construction; callers own source selection and physical IO.
func LoadProjectDocuments(projectID string, config *execution.ProjectConfig, documents []SourceDocument) (*ProjectSource, error) {
	return loadProjectDocuments(projectID, config, documents, nil)
}

func loadProjectDocuments(projectID string, config *execution.ProjectConfig, documents []SourceDocument, locations map[string]string) (*ProjectSource, error) {
	if strings.TrimSpace(projectID) == "" || config == nil {
		return nil, &LoadError{Code: DiagnosticProjectConfig, Err: fmt.Errorf("project and config are required")}
	}
	// Own all inputs, including policy maps and source bytes.
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	config, err = execution.LoadProjectConfig(encoded)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticProjectConfig, Err: err}
	}
	documents = append([]SourceDocument(nil), documents...)
	sort.Slice(documents, func(i, j int) bool {
		if documents[i].Source != documents[j].Source {
			return documents[i].Source < documents[j].Source
		}
		return documents[i].Path < documents[j].Path
	})
	bySource := make(map[string][]SourceDocument)
	for i, document := range documents {
		if _, ok := config.SemanticSources[document.Source]; !ok || document.Path == "" ||
			(i > 0 && documents[i-1].Source == document.Source && documents[i-1].Path == document.Path) {
			return nil, &LoadError{Code: DiagnosticSourceResolution, Err: fmt.Errorf("invalid source identity")}
		}
		if document.Digest != "" && (document.Digest != digestBytes(document.Content) || document.Size != int64(len(document.Content))) {
			return nil, &LoadError{Code: DiagnosticSourceResolution, Err: fmt.Errorf("source integrity mismatch")}
		}
		bySource[document.Source] = append(bySource[document.Source], document)
	}
	combined := &ossie.Document{Name: projectID}
	bundle := ImmutableSemanticBundle{SchemaVersion: BundleSchemaVersion, ProjectID: projectID}
	assetLocations := map[string]string{}

	keys := make([]string, 0, len(config.SemanticSources))
	for key := range config.SemanticSources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		source := config.SemanticSources[key]
		bundle.Sources = append(bundle.Sources, SourceSpec{Name: key, Pattern: filepath.ToSlash(filepath.Clean(strings.TrimSpace(source.Path)))})
		if len(bySource[key]) == 0 {
			return nil, &LoadError{Code: DiagnosticSourceResolution, Location: key, Err: fmt.Errorf("source matched no documents")}
		}
		for _, document := range bySource[key] {
			path, body := document.Path, document.Content
			location := path
			if local, ok := locations[path]; ok {
				location = local
			}
			doc, err := ossie.NewLoader().Load(body)
			if err != nil {
				return nil, &LoadError{Code: DiagnosticDocumentInvalid, Location: location, Err: err}
			}
			if err := mergeProjectDocument(combined, doc, location); err != nil {
				return nil, &LoadError{Code: DiagnosticAssemblyInvalid, Location: location, Err: err}
			}
			identity := path
			for n := range doc.OntologyMappings {
				assetLocations[fmt.Sprintf("ontology_mappings[%d]", len(combined.OntologyMappings)-len(doc.OntologyMappings)+n)] = identity
			}
			indexDocumentLocations(doc, identity, assetLocations)
			bundle.Documents = append(bundle.Documents, SourceDocument{
				Source: key, Path: identity, Digest: digestBytes(body), Size: int64(len(body)), Content: append([]byte(nil), body...),
			})
		}
	}
	bundle.OssieVersion = combined.Version
	bundle.ContentDigest, err = ComputeContentDigest(bundle)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticAssemblyInvalid, Err: err}
	}
	semanticManifest, err := manifest.BuildProjectManifest(projectID, combined)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticManifestInvalid, Err: err}
	}
	candidate := &ProjectSource{Bundle: bundle, Config: config, Document: combined, Manifest: semanticManifest, assetLocations: assetLocations}
	candidate.Quality, err = EvaluateQuality(candidate, config.Quality)
	if err != nil {
		return nil, &LoadError{Code: DiagnosticQualityEvaluation, Err: err}
	}
	return candidate, nil
}

func ValidateProject(projectID, configPath string) ValidationResult {
	result := ValidationResult{SchemaVersion: ValidationSchemaVersion, ProjectID: strings.TrimSpace(projectID), Diagnostics: []Diagnostic{}}
	candidate, err := LoadProject(projectID, configPath)
	if err == nil {
		result.Valid = true
		result.Publishable = candidate.Quality.Publishable
		result.QualityPublicationThreshold = candidate.Quality.PublicationThreshold
		result.ContentDigest = candidate.Bundle.ContentDigest
		result.ManifestDigest = candidate.Manifest.Digest
		result.Diagnostics = append(result.Diagnostics, candidate.Quality.Diagnostics...)
		return result
	}
	code, location := DiagnosticManifestInvalid, ""
	if candidateErr, ok := err.(*LoadError); ok {
		code, location = candidateErr.Code, candidateErr.Location
	}
	result.Diagnostics = append(result.Diagnostics, Diagnostic{
		Code: code, Severity: SeverityError, Location: diagnosticLocation(location), Message: err.Error(), CallerAction: serrors.CallerActionChangeModel,
	})
	return result
}

func indexDocumentLocations(document *ossie.Document, path string, locations map[string]string) {
	if document == nil {
		return
	}
	for _, concept := range document.Ontology {
		locations["ontology_concept:"+ontologyConceptName(concept)] = path
	}
	for modelIndex := range document.SemanticModel {
		model := &document.SemanticModel[modelIndex]
		locations["model:"+model.Name] = path
		for datasetIndex := range model.Datasets {
			dataset := &model.Datasets[datasetIndex]
			locations[datasetRef(model.Name, dataset.Name)] = path
			for fieldIndex := range dataset.Fields {
				field := &dataset.Fields[fieldIndex]
				if field.Dimension != nil {
					locations[dimensionRef(model.Name, dataset.Name, field.Name)] = path
				}
			}
		}
		for metricIndex := range model.Metrics {
			metric := &model.Metrics[metricIndex]
			locations[metricRef(model.Name, metric.Name)] = path
		}
		for relationshipIndex := range model.Relationships {
			relationship := &model.Relationships[relationshipIndex]
			locations["relationship:"+model.Name+"."+relationship.Name] = path
		}
	}
}

func sourceIdentityPath(baseDir, path string) string {
	relative, err := filepath.Rel(baseDir, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Clean(relative))
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		return filepath.ToSlash(filepath.Clean(absolute))
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func expandSemanticSourcePaths(baseDir string, source execution.SemanticSourceConfig) ([]string, error) {
	pattern := strings.TrimSpace(source.Path)
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(baseDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid semantic source path pattern %q: %w", source.Path, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("semantic source path pattern %q matched no files", source.Path)
	}
	for i, match := range matches {
		absolute, err := filepath.Abs(match)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("semantic source %q is not a regular file", absolute)
		}
		matches[i] = absolute
	}
	sort.Strings(matches)
	return matches, nil
}

func mergeProjectDocument(combined, doc *ossie.Document, path string) error {
	if combined.Version == "" {
		combined.Version = doc.Version
	} else if doc.Version != combined.Version {
		return fmt.Errorf("Ossie version mismatch in %q: %s != %s", path, doc.Version, combined.Version)
	}
	combined.Requires = appendUniqueStrings(combined.Requires, doc.Requires...)

	seenModels := make(map[string]struct{}, len(combined.SemanticModel))
	for _, model := range combined.SemanticModel {
		seenModels[model.Name] = struct{}{}
	}
	for _, model := range doc.SemanticModel {
		if _, exists := seenModels[model.Name]; exists {
			return fmt.Errorf("duplicate SemanticModel %q in %q", model.Name, path)
		}
		seenModels[model.Name] = struct{}{}
		combined.SemanticModel = append(combined.SemanticModel, model)
	}

	seenConcepts := make(map[string]struct{}, len(combined.Ontology))
	for _, concept := range combined.Ontology {
		if name := ontologyConceptName(concept); name != "" {
			seenConcepts[name] = struct{}{}
		}
	}
	for _, concept := range doc.Ontology {
		name := ontologyConceptName(concept)
		if name != "" {
			if _, exists := seenConcepts[name]; exists {
				return fmt.Errorf("duplicate Ossie ontology concept %q in %q", strings.TrimSpace(stringField(concept, "concept")), path)
			}
			seenConcepts[name] = struct{}{}
		}
		combined.Ontology = append(combined.Ontology, concept)
	}

	seenMappings := make(map[string]struct{}, len(combined.OntologyMappings))
	for _, mapping := range combined.OntologyMappings {
		key, err := canonicalDocumentEntry(mapping)
		if err != nil {
			return fmt.Errorf("canonicalize existing Ossie ontology mapping: %w", err)
		}
		seenMappings[key] = struct{}{}
	}
	for _, mapping := range doc.OntologyMappings {
		key, err := canonicalDocumentEntry(mapping)
		if err != nil {
			return fmt.Errorf("canonicalize Ossie ontology mapping in %q: %w", path, err)
		}
		if _, exists := seenMappings[key]; exists {
			return fmt.Errorf("duplicate Ossie ontology mapping in %q", path)
		}
		seenMappings[key] = struct{}{}
		combined.OntologyMappings = append(combined.OntologyMappings, mapping)
	}
	return nil
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func ontologyConceptName(concept map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringField(concept, "concept")))
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func canonicalDocumentEntry(value map[string]any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
