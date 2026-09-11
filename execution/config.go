package execution

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meaningforge/metis/serrors"
	"go.yaml.in/yaml/v3"
)

// ProjectConfig is semantic packaging only. Project identity and runtime wiring
// are owned by the root deployment registration in metis.yaml.
type ProjectConfig struct {
	SemanticSources map[string]SemanticSourceConfig `yaml:"semantic_sources" json:"semantic_sources"`
	Quality         QualityPolicyConfig             `yaml:"quality,omitempty" json:"quality,omitempty"`
}

type SemanticSourceConfig struct {
	Path string `yaml:"path" json:"path"`
}

// QualityPolicyConfig controls the severity and publication threshold of
// deterministic advisory model-quality diagnostics. It does not change Ossie
// loading or semantic interpretation.
type QualityPolicyConfig struct {
	Severities           map[string]QualitySeverity `yaml:"severities,omitempty" json:"severities,omitempty"`
	PublicationThreshold QualitySeverity            `yaml:"publication_threshold,omitempty" json:"publication_threshold,omitempty"`
}

// QualitySeverity is the closed project-policy severity vocabulary.
type QualitySeverity string

const (
	QualitySeverityError   QualitySeverity = "error"
	QualitySeverityWarning QualitySeverity = "warning"
	QualitySeverityInfo    QualitySeverity = "info"
)

func LoadProjectConfigFile(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadProjectConfig(data)
}

func LoadProjectConfig(data []byte) (*ProjectConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg ProjectConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidExecutionConfig, Message: "failed to parse project config", Details: map[string]any{"cause": err.Error()}}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *ProjectConfig) Validate() error {
	if c == nil {
		return invalidConfig("project config is required", nil)
	}
	if len(c.SemanticSources) == 0 {
		return invalidConfig("project config must define at least one semantic source", nil)
	}
	seenPaths := map[string]string{}
	for key, source := range c.SemanticSources {
		key = strings.TrimSpace(key)
		if key == "" {
			return invalidConfig("semantic source key cannot be empty", nil)
		}
		path := strings.TrimSpace(source.Path)
		if path == "" {
			return invalidConfig("semantic source path is required", map[string]any{"source": key})
		}
		canonical := filepath.Clean(path)
		if previous, exists := seenPaths[canonical]; exists {
			return invalidConfig("duplicate semantic source path", map[string]any{"source": key, "previous_source": previous, "path": path})
		}
		seenPaths[canonical] = key
	}
	qualityCodes := make([]string, 0, len(c.Quality.Severities))
	for code := range c.Quality.Severities {
		qualityCodes = append(qualityCodes, code)
	}
	sort.Strings(qualityCodes)
	for _, code := range qualityCodes {
		severity := c.Quality.Severities[code]
		if strings.TrimSpace(code) == "" {
			return invalidConfig("quality severity code cannot be empty", nil)
		}
		severity = QualitySeverity(strings.ToLower(strings.TrimSpace(string(severity))))
		if !validQualitySeverity(severity) {
			return invalidConfig("quality severity must be error, warning, or info", map[string]any{"code": code, "severity": severity})
		}
		c.Quality.Severities[code] = severity
	}
	threshold := QualitySeverity(strings.ToLower(strings.TrimSpace(string(c.Quality.PublicationThreshold))))
	if threshold != "" && !validQualitySeverity(threshold) {
		return invalidConfig("quality publication threshold must be error, warning, or info", map[string]any{"publication_threshold": threshold})
	}
	c.Quality.PublicationThreshold = threshold
	return nil
}

func validQualitySeverity(value QualitySeverity) bool {
	switch value {
	case QualitySeverityError, QualitySeverityWarning, QualitySeverityInfo:
		return true
	default:
		return false
	}
}

func invalidConfig(message string, details map[string]any) error {
	return &serrors.Error{Code: serrors.ErrInvalidExecutionConfig, Message: message, Details: details}
}
