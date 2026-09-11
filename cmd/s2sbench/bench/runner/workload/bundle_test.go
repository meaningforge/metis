package workload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/meaningforge/metis/query"
)

func TestBundleSealLoadAndTamperDetection(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"models/a.yaml", "catalog/catalog.json", "knowledge/index.md", "knowledge/tables/a.md", "data/a.csv", "data/a.duckdb", "data/schema.sql", "data/load.sql", "cases/q1/reference.sql"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(path, format string) Asset {
		digest, err := FileDigest(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return Asset{Path: path, SHA256: digest, Format: format}
	}
	oracle := scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "value", ValueKind: scenarios.ResultInteger}}}, Comparison: scenarios.ResultUnordered}
	bundle := Bundle{
		SchemaVersion: SchemaVersion, Name: "custom", Provider: "test", ProviderVersion: "v1", Engine: "duckdb", Scale: 1,
		DataProvenance: DataProvenance{Classification: "synthetic", Generator: "test-generator", Version: "v1"},
		Models:         []ModelAsset{{Asset: asset("models/a.yaml", "ossie"), Project: "p", Model: "m"}},
		Knowledge: KnowledgeBundle{Format: "okf", Version: "0.2", Revision: "rev", ProjectionVersion: "catalog-v1", Catalog: asset("catalog/catalog.json", "json"), Files: []Asset{asset("knowledge/index.md", "markdown"), asset("knowledge/tables/a.md", "markdown")},
			Enrichment: &KnowledgeEnrichment{Workflow: "google-okf-reference-agent-v1", Provider: "test", Model: "test-model", Skills: []string{"list_concepts"}, PromptSHA256: "sha256:prompt", OutputSHA256: "sha256:output"}},
		Data: []Asset{asset("data/a.csv", "csv")}, Database: asset("data/a.duckdb", "duckdb"), Schema: asset("data/schema.sql", "sql"), Loader: asset("data/load.sql", "sql"),
		Cases: []Case{{
			Name: "q1", Stratum: "control", Question: "Count it",
			Query:        query.SemanticQuery{Project: "p", Model: "m", Metrics: []query.MetricRef{{Name: "count"}}},
			ReferenceSQL: asset("cases/q1/reference.sql", "sql"), Oracle: oracle,
			OracleAuthority: "provider-reference-sql", CrossValidation: "metis-match",
		}},
	}
	if err := bundle.Seal(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(root, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(bundle); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "data/a.csv"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("tampered Load() error = %v, want digest rejection", err)
	}
}

func TestBundleAllowsKnowledgeFoundation(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"models/a.yaml", "data/a.csv", "data/a.duckdb", "data/schema.sql", "data/load.sql", "cases/q1/reference.sql"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(path, format string) Asset {
		digest, err := FileDigest(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return Asset{Path: path, SHA256: digest, Format: format}
	}
	oracle := scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "value", ValueKind: scenarios.ResultInteger}}}, Comparison: scenarios.ResultUnordered}
	bundle := Bundle{
		SchemaVersion: SchemaVersion, Name: "foundation", Provider: "test", ProviderVersion: "v1", Engine: "duckdb", Scale: 1,
		DataProvenance: DataProvenance{Classification: "synthetic", Generator: "test-generator", Version: "v1"},
		Models:         []ModelAsset{{Asset: asset("models/a.yaml", "ossie"), Project: "p", Model: "m"}},
		Data:           []Asset{asset("data/a.csv", "csv")}, Database: asset("data/a.duckdb", "duckdb"), Schema: asset("data/schema.sql", "sql"), Loader: asset("data/load.sql", "sql"),
		Cases: []Case{{Name: "q1", Stratum: "control", Question: "Count it", Query: query.SemanticQuery{Project: "p", Model: "m", Metrics: []query.MetricRef{{Name: "count"}}}, ReferenceSQL: asset("cases/q1/reference.sql", "sql"), Oracle: oracle, OracleAuthority: "provider-reference-sql", CrossValidation: "metis-match"}},
	}
	if err := bundle.Seal(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(root, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(bundle); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err != nil {
		t.Fatalf("Load() foundation error = %v", err)
	}
}

func TestResolveAssetRejectsEscape(t *testing.T) {
	if _, err := ResolveAsset(t.TempDir(), "../oracle.json"); err == nil {
		t.Fatal("ResolveAsset accepted traversal")
	}
}
