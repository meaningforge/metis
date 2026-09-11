// Package workload defines self-contained, reproducible S2SBench suites.
package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/query"
)

const (
	SchemaVersion = "s2sbench-workload-v4"
	ManifestFile  = "suite.json"
)

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Format string `json:"format,omitempty"`
}

type ModelAsset struct {
	Asset
	Project string `json:"project"`
	Model   string `json:"model"`
}

type KnowledgeBundle struct {
	Format            string               `json:"format"`
	Version           string               `json:"version"`
	Revision          string               `json:"revision"`
	ProjectionVersion string               `json:"projection_version"`
	Catalog           Asset                `json:"catalog"`
	Files             []Asset              `json:"files"`
	Enrichment        *KnowledgeEnrichment `json:"enrichment,omitempty"`
}

// KnowledgeEnrichment records the bounded, per-concept agent pass that turned
// a catalog projection into an OKF treatment. The generated prose is frozen in
// the bundle; this provenance is for review and repeatability, not authority.
type KnowledgeEnrichment struct {
	Workflow     string   `json:"workflow"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Skills       []string `json:"skills"`
	PromptSHA256 string   `json:"prompt_sha256"`
	OutputSHA256 string   `json:"output_sha256"`
}

type DataProvenance struct {
	Classification string `json:"classification"`
	Specification  string `json:"specification,omitempty"`
	Generator      string `json:"generator"`
	Version        string `json:"version"`
}

type Case struct {
	Name            string                      `json:"name"`
	Stratum         string                      `json:"stratum"`
	Question        string                      `json:"question"`
	Query           query.SemanticQuery         `json:"query"`
	ReferenceSQL    Asset                       `json:"reference_sql"`
	Oracle          scenarios.ResultExpectation `json:"oracle"`
	OracleAuthority string                      `json:"oracle_authority"`
	CrossValidation string                      `json:"cross_validation"`
}

type Bundle struct {
	SchemaVersion   string          `json:"schema_version"`
	Name            string          `json:"name"`
	Provider        string          `json:"provider"`
	ProviderVersion string          `json:"provider_version"`
	Engine          string          `json:"engine"`
	Scale           float64         `json:"scale"`
	DataProvenance  DataProvenance  `json:"data_provenance"`
	Source          string          `json:"source"`
	SourceRevision  string          `json:"source_revision"`
	SourceSHA256    string          `json:"source_sha256"`
	Models          []ModelAsset    `json:"models"`
	Knowledge       KnowledgeBundle `json:"knowledge"`
	Data            []Asset         `json:"data"`
	Database        Asset           `json:"database"`
	Schema          Asset           `json:"schema"`
	Loader          Asset           `json:"loader"`
	Cases           []Case          `json:"cases"`
	Digest          string          `json:"digest"`
}

func (b Bundle) Validate() error {
	if b.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported workload schema %q", b.SchemaVersion)
	}
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Provider) == "" || strings.TrimSpace(b.ProviderVersion) == "" {
		return fmt.Errorf("workload name, provider, and provider version are required")
	}
	if strings.TrimSpace(b.Engine) == "" || strings.TrimSpace(b.DataProvenance.Classification) == "" || strings.TrimSpace(b.DataProvenance.Generator) == "" || strings.TrimSpace(b.DataProvenance.Version) == "" {
		return fmt.Errorf("workload engine and complete data provenance are required")
	}
	if b.Scale <= 0 || len(b.Models) == 0 || len(b.Data) == 0 || len(b.Cases) == 0 {
		return fmt.Errorf("workload requires positive scale, models, data, and cases")
	}
	if b.Knowledge.Format != "" {
		if b.Knowledge.Format != "okf" || strings.TrimSpace(b.Knowledge.Version) == "" || strings.TrimSpace(b.Knowledge.Revision) == "" || strings.TrimSpace(b.Knowledge.ProjectionVersion) == "" || strings.TrimSpace(b.Knowledge.Catalog.Path) == "" || len(b.Knowledge.Files) == 0 {
			return fmt.Errorf("workload knowledge bundle is incomplete")
		}
		if b.Knowledge.Enrichment == nil || strings.TrimSpace(b.Knowledge.Enrichment.Workflow) == "" || strings.TrimSpace(b.Knowledge.Enrichment.Provider) == "" || strings.TrimSpace(b.Knowledge.Enrichment.Model) == "" || len(b.Knowledge.Enrichment.Skills) == 0 || strings.TrimSpace(b.Knowledge.Enrichment.PromptSHA256) == "" || strings.TrimSpace(b.Knowledge.Enrichment.OutputSHA256) == "" {
			return fmt.Errorf("workload knowledge enrichment provenance is incomplete")
		}
	} else if b.Knowledge.Version != "" || b.Knowledge.Revision != "" || b.Knowledge.ProjectionVersion != "" || b.Knowledge.Catalog.Path != "" || len(b.Knowledge.Files) != 0 || b.Knowledge.Enrichment != nil {
		return fmt.Errorf("workload knowledge foundation mixes empty and populated fields")
	}
	seen := make(map[string]struct{}, len(b.Cases))
	for index, c := range b.Cases {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Question) == "" || c.Query.Model == "" || c.Oracle.Columns == nil ||
			strings.TrimSpace(c.ReferenceSQL.Path) == "" || strings.TrimSpace(c.OracleAuthority) == "" || c.CrossValidation != "metis-match" {
			return fmt.Errorf("workload case %d is incomplete", index+1)
		}
		if _, duplicate := seen[c.Name]; duplicate {
			return fmt.Errorf("workload case %q is duplicated", c.Name)
		}
		seen[c.Name] = struct{}{}
	}
	want, err := b.canonicalDigest()
	if err != nil {
		return err
	}
	if b.Digest != want {
		return fmt.Errorf("workload digest %q, want %q", b.Digest, want)
	}
	return nil
}

func (b Bundle) canonicalDigest() (string, error) {
	b.Digest = ""
	body, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (b *Bundle) Seal() error {
	digest, err := b.canonicalDigest()
	if err != nil {
		return err
	}
	b.Digest = digest
	return b.Validate()
}

func Load(root string) (Bundle, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Bundle{}, err
	}
	file, err := os.Open(filepath.Join(root, ManifestFile))
	if err != nil {
		return Bundle{}, fmt.Errorf("open workload manifest: %w", err)
	}
	defer file.Close()
	var bundle Bundle
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("decode workload manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Bundle{}, fmt.Errorf("workload manifest contains trailing JSON")
	}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, err
	}
	assets := append([]Asset{}, bundle.Data...)
	if bundle.Knowledge.Format != "" {
		assets = append(assets, bundle.Knowledge.Catalog)
		assets = append(assets, bundle.Knowledge.Files...)
	}
	for _, model := range bundle.Models {
		assets = append(assets, model.Asset)
	}
	assets = append(assets, bundle.Database, bundle.Schema, bundle.Loader)
	for _, benchmarkCase := range bundle.Cases {
		assets = append(assets, benchmarkCase.ReferenceSQL)
	}
	for _, asset := range assets {
		path, err := ResolveAsset(root, asset.Path)
		if err != nil {
			return Bundle{}, err
		}
		got, err := FileDigest(path)
		if err != nil {
			return Bundle{}, err
		}
		if got != asset.SHA256 {
			return Bundle{}, fmt.Errorf("workload asset %q digest %q, want %q", asset.Path, got, asset.SHA256)
		}
	}
	return bundle, nil
}

func ResolveAsset(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || relative == "" {
		return "", fmt.Errorf("workload asset path %q must be relative", relative)
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workload asset path %q escapes its root", relative)
	}
	return filepath.Join(root, clean), nil
}

func (b Bundle) Definitions(root string) ([]fixtures.Definition, error) {
	definitions := make([]fixtures.Definition, 0, len(b.Models))
	for index, asset := range b.Models {
		path, err := ResolveAsset(root, asset.Path)
		if err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(asset.Project) == "" || strings.TrimSpace(asset.Model) == "" {
			return nil, fmt.Errorf("workload model asset %d has no project/model identity", index)
		}
		definitions = append(definitions, fixtures.Definition{ID: fixtures.ID(fmt.Sprintf("workload_%d", index)), Project: asset.Project, Model: asset.Model, Document: body})
	}
	return definitions, nil
}

func FileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open workload asset %q: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
