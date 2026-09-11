package source

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const (
	BundleSchemaVersion     = 1
	DescriptorSchemaVersion = 1
	ValidationSchemaVersion = 2
	DiffSchemaVersion       = 1
	QualitySchemaVersion    = 1
)

type SourceSpec struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

type SourceDocument struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Size    int64  `json:"size"`
	Content []byte `json:"content"`
}

type SourceIdentity struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type ImmutableSemanticBundle struct {
	SchemaVersion int              `json:"schema_version"`
	ProjectID     string           `json:"project_id"`
	OssieVersion  string           `json:"ossie_version"`
	Sources       []SourceSpec     `json:"sources"`
	Documents     []SourceDocument `json:"documents"`
	ContentDigest string           `json:"content_digest"`
}

type ProjectSource struct {
	Bundle         ImmutableSemanticBundle
	Config         *execution.ProjectConfig
	Document       *ossie.Document
	Manifest       *manifest.SemanticManifest
	Quality        QualityReport
	assetLocations map[string]string
}

type Diagnostic struct {
	Code          DiagnosticCode       `json:"code"`
	Severity      DiagnosticSeverity   `json:"severity"`
	Asset         string               `json:"asset,omitempty"`
	RelatedAssets []string             `json:"related_assets,omitempty"`
	Location      *DiagnosticLocation  `json:"location,omitempty"`
	Message       string               `json:"message"`
	CallerAction  serrors.CallerAction `json:"caller_action"`
	Evidence      *DiagnosticEvidence  `json:"evidence,omitempty"`
}

// DiagnosticCode identifies a registered authoring or model-quality result.
type DiagnosticCode string

// DiagnosticReason identifies the typed evidence variant carried by one code.
type DiagnosticReason string

// DiagnosticEvidence is the closed JSON vocabulary shared by quality rules.
// Fields are populated according to Code and Reason; clients never need to
// recover machine values by parsing Message.
type DiagnosticEvidence struct {
	Reason               DiagnosticReason              `json:"reason"`
	DefinitionDigest     string                        `json:"definition_digest,omitempty"`
	References           []string                      `json:"references,omitempty"`
	CanonicalReferences  []string                      `json:"canonical_references,omitempty"`
	SourceLocations      map[string]DiagnosticLocation `json:"source_locations,omitempty"`
	NormalizedIdentity   string                        `json:"normalized_identity,omitempty"`
	MetricSourceDatasets []string                      `json:"metric_source_datasets,omitempty"`
	Dataset              string                        `json:"dataset,omitempty"`
	SourceDataset        string                        `json:"source_dataset,omitempty"`
	TargetDataset        string                        `json:"target_dataset,omitempty"`
	CauseCode            serrors.ErrorCode             `json:"cause_code,omitempty"`
	DeclaredType         ossie.DataType                `json:"declared_type,omitempty"`
	ExpectedType         ossie.DataType                `json:"expected_type,omitempty"`
	ExpressionTypes      []expression.SemanticType     `json:"expression_types,omitempty"`
	AggregationStates    []expression.AggregationState `json:"aggregation_states,omitempty"`
	ExpectedAggregation  ossie.QualityAggregation      `json:"expected_aggregation,omitempty"`
	Owner                string                        `json:"owner,omitempty"`
	Lifecycle            ossie.AssetLifecycle          `json:"lifecycle,omitempty"`
	Certification        ossie.AssetCertification      `json:"certification,omitempty"`
	DeprecationDate      string                        `json:"deprecation_date,omitempty"`
	Replacement          string                        `json:"replacement,omitempty"`
}

// DiagnosticSeverity is a closed machine-readable severity vocabulary.
type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityInfo    DiagnosticSeverity = "info"
)

// DiagnosticLocation keeps source coordinates structured. Line and column are
// one-based when present; current Ossie loading always provides Path and may
// add exact coordinates without changing the JSON shape.
type DiagnosticLocation struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type QualityReport struct {
	SchemaVersion        int                `json:"schema_version"`
	ProjectID            string             `json:"project_id"`
	ContentDigest        string             `json:"content_digest"`
	PublicationThreshold DiagnosticSeverity `json:"publication_threshold"`
	Publishable          bool               `json:"publishable"`
	Diagnostics          []Diagnostic       `json:"diagnostics"`
}

type ValidationResult struct {
	SchemaVersion               int                `json:"schema_version"`
	ProjectID                   string             `json:"project_id"`
	ContentDigest               string             `json:"content_digest,omitempty"`
	ManifestDigest              string             `json:"manifest_digest,omitempty"`
	Valid                       bool               `json:"valid"`
	Publishable                 bool               `json:"publishable"`
	QualityPublicationThreshold DiagnosticSeverity `json:"quality_publication_threshold,omitempty"`
	Diagnostics                 []Diagnostic       `json:"diagnostics"`
}

type LoadError struct {
	Code     DiagnosticCode
	Location string
	Err      error
}

func (e *LoadError) Error() string {
	if e == nil {
		return ""
	}
	if e.Location == "" {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Location, e.Err)
}

func (e *LoadError) Unwrap() error { return e.Err }

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func framedDigest(parts ...[]byte) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%d:", len(part))
		_, _ = hash.Write(part)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func ComputeContentDigest(bundle ImmutableSemanticBundle) (string, error) {
	parts := [][]byte{[]byte("metis-semantic-bundle-v1"), []byte(bundle.OssieVersion)}
	for _, source := range bundle.Sources {
		encoded, err := json.Marshal(source)
		if err != nil {
			return "", err
		}
		parts = append(parts, encoded)
	}
	for _, document := range bundle.Documents {
		parts = append(parts, []byte(document.Source), []byte(document.Path), document.Content)
	}
	return framedDigest(parts...), nil
}
