package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func TestResolveExecutableUsesPATHForBareName(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-duckdb")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := resolveExecutable("fake-duckdb", "duckdb")
	if err != nil {
		t.Fatal(err)
	}
	if got != binary {
		t.Fatalf("resolveExecutable=%q, want %q", got, binary)
	}
}

func TestResolveExecutableValidatesExplicitPath(t *testing.T) {
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "duckdb")
	if err := os.WriteFile(notExecutable, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveExecutable(notExecutable, "duckdb"); err == nil {
		t.Fatal("resolveExecutable accepted a non-executable path")
	}
}

func TestValidateAgentProviderRejectsCodexMetadataMismatch(t *testing.T) {
	if err := validateAgentProvider("codex", "openai"); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentProvider("codex", "anthropic"); err == nil {
		t.Fatal("validateAgentProvider accepted a Codex provider mismatch")
	}
	if err := validateAgentProvider("generic", "custom"); err != nil {
		t.Fatalf("generic provider was over-constrained: %v", err)
	}
}

func TestNormalizeRunSelectionMakesScenarioASingleRepetitionSmoke(t *testing.T) {
	scenario, repetitions, err := normalizeRunSelection(" conversion_rate_by_campaign ", s2sbench.FrozenBudget.Repetitions, false)
	if err != nil {
		t.Fatal(err)
	}
	if scenario != "conversion_rate_by_campaign" || repetitions != 1 {
		t.Fatalf("selection=%q/%d, want trimmed scenario/1", scenario, repetitions)
	}
	if _, _, err := normalizeRunSelection(scenario, s2sbench.FrozenBudget.Repetitions, true); err == nil {
		t.Fatal("explicit five repetitions were accepted with --scenario")
	}
}

func TestValidateRepairSelectionAllowsDisablingRepairOnlyForSmoke(t *testing.T) {
	for _, repairAttempts := range []int{0, 1} {
		if err := validateRepairSelection("aggregation_variants", repairAttempts); err != nil {
			t.Fatalf("smoke repair attempts %d rejected: %v", repairAttempts, err)
		}
	}
	if err := validateRepairSelection("", 0); err == nil {
		t.Fatal("formal run accepted disabled repair")
	}
	for _, repairAttempts := range []int{-1, 2} {
		if err := validateRepairSelection("aggregation_variants", repairAttempts); err == nil {
			t.Fatalf("smoke accepted repair attempts %d", repairAttempts)
		}
	}
}

func TestConfiguredPiAuthFileUsesConfiguredPiDirectory(t *testing.T) {
	directory := t.TempDir()
	authFile := filepath.Join(directory, "auth.json")
	if err := os.WriteFile(authFile, []byte(`{"deepseek":{"api_key":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", directory)
	if got := configuredPiAuthFile(); got != authFile {
		t.Fatalf("configured Pi auth file = %q, want %q", got, authFile)
	}
}

func TestDefaultOutputPathIsRepositoryIndependent(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	output, err := defaultOutputPath()
	if err != nil {
		t.Fatal(err)
	}
	wantParent := filepath.Join(root, "s2sbench-results")
	if filepath.Dir(output) != wantParent || !strings.HasPrefix(filepath.Base(output), "run-") {
		t.Fatalf("default output = %q, want generated child of %q", output, wantParent)
	}
}

func TestWriteJSONFileRedactsKnownSecrets(t *testing.T) {
	t.Setenv("S2SBENCH_TEST_TOKEN", "persisted-secret-value")
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := writeJSONFile(path, map[string]string{"message": "failed with persisted-secret-value", "access_token": "another-secret"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "persisted-secret-value") || strings.Contains(string(body), "another-secret") {
		t.Fatalf("artifact leaked a secret: %s", body)
	}
}

func TestPersistRunBundleCommitsCompleteBundleAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "run-001")
	manifest := s2sbench.RunManifest{SchemaVersion: "test"}
	collection := s2sbench.Collection{
		RawAssets: s2sbench.ArmRunArtifact{Manifest: manifest, Path: s2sbench.PathRawAssets},
		Metis:     s2sbench.ArmRunArtifact{Manifest: manifest, Path: s2sbench.PathMetis},
	}
	report := s2sbench.V0Report{Manifest: manifest}
	if err := persistRunBundle(output, manifest, collection, report); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "attempts.jsonl", "attempts.log", "raw-assets.json", "metis.json", "report.json", "summary.log"} {
		info, err := os.Stat(filepath.Join(output, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o, want 600", name, info.Mode().Perm())
		}
	}
	if err := persistRunBundle(output, manifest, collection, report); err == nil {
		t.Fatal("persistRunBundle overwrote an existing result directory")
	}
}
