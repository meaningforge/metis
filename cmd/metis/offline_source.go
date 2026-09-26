package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/service/source"
)

func validateProject(args []string) int {
	fs := flag.NewFlagSet("metis project validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "stable project ID")
	config := fs.String("config", "", "semantic project manifest")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*config) == "" {
		fmt.Fprintln(os.Stderr, "metis project validate: --project and --config are required")
		return 2
	}
	result := source.ValidateProject(*project, *config)
	if err := writeCLIJSON(result); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write validation result: %v\n", err)
		return 1
	}
	if !result.Valid || !result.Publishable {
		return 1
	}
	return 0
}

func inspectProject(args []string) int {
	fs := flag.NewFlagSet("metis project inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "stable project ID")
	config := fs.String("config", "", "semantic project manifest")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*config) == "" {
		fmt.Fprintln(os.Stderr, "metis project inspect: --project and --config are required")
		return 2
	}
	candidate, err := source.LoadProject(*project, *config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: inspect project: %v\n", err)
		return 1
	}
	type modelSummary struct {
		Name          string   `json:"name"`
		Datasets      []string `json:"datasets"`
		Metrics       []string `json:"metrics"`
		Relationships []string `json:"relationships"`
	}
	out := struct {
		SchemaVersion  int                     `json:"schema_version"`
		ProjectID      string                  `json:"project_id"`
		ContentDigest  string                  `json:"content_digest"`
		ManifestDigest string                  `json:"manifest_digest"`
		OssieVersion   string                  `json:"ossie_version"`
		Sources        []source.SourceIdentity `json:"sources"`
		Models         []modelSummary          `json:"models"`
		Quality        source.QualityReport    `json:"quality"`
	}{SchemaVersion: 2, ProjectID: candidate.Bundle.ProjectID, ContentDigest: candidate.Bundle.ContentDigest, ManifestDigest: candidate.Manifest.Digest, OssieVersion: candidate.Bundle.OssieVersion, Quality: candidate.Quality}
	for _, document := range candidate.Bundle.Documents {
		out.Sources = append(out.Sources, source.SourceIdentity{Source: document.Source, Path: document.Path, Digest: document.Digest, Size: document.Size})
	}
	for _, model := range candidate.Document.SemanticModel {
		summary := modelSummary{Name: model.Name}
		for _, dataset := range model.Datasets {
			summary.Datasets = append(summary.Datasets, dataset.Name)
		}
		for _, metric := range model.Metrics {
			summary.Metrics = append(summary.Metrics, metric.Name)
		}
		for _, relationship := range model.Relationships {
			summary.Relationships = append(summary.Relationships, relationship.Name)
		}
		sort.Strings(summary.Datasets)
		sort.Strings(summary.Metrics)
		sort.Strings(summary.Relationships)
		out.Models = append(out.Models, summary)
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Name < out.Models[j].Name })
	if err := writeCLIJSON(out); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write project inspection: %v\n", err)
		return 1
	}
	return 0
}

func diffProject(args []string) int {
	fs := flag.NewFlagSet("metis project diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "stable project ID")
	baseConfig := fs.String("base-config", "", "base semantic project manifest")
	candidateConfig := fs.String("candidate-config", "", "candidate semantic project manifest")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*baseConfig) == "" || strings.TrimSpace(*candidateConfig) == "" {
		fmt.Fprintln(os.Stderr, "metis project diff: --project, --base-config, and --candidate-config are required")
		return 2
	}
	base, err := source.LoadProject(*project, *baseConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: load base project: %v\n", err)
		return 1
	}
	candidate, err := source.LoadProject(*project, *candidateConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: load candidate project: %v\n", err)
		return 1
	}
	diff, err := source.Compare(base, candidate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: compare semantic projects: %v\n", err)
		return 1
	}
	if err := writeCLIJSON(diff); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write semantic diff: %v\n", err)
		return 1
	}
	return 0
}

func formatModel(args []string) int {
	fs := flag.NewFlagSet("metis model format", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	model := fs.String("model", "", "Apache Ossie YAML/JSON model file")
	output := fs.String("output", "", "new path for deterministic formatted output")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(*model) == "" || strings.TrimSpace(*output) == "" {
		fmt.Fprintln(os.Stderr, "metis model format: --model and --output are required")
		return 2
	}
	body, err := os.ReadFile(*model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: read model: %v\n", err)
		return 1
	}
	formatted, err := source.FormatDocument(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: format model: %v\n", err)
		return 1
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: create formatted model: %v\n", err)
		return 1
	}
	if _, err := file.Write(formatted); err != nil {
		_ = file.Close()
		_ = os.Remove(*output)
		fmt.Fprintf(os.Stderr, "ERROR: write formatted model: %v\n", err)
		return 1
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(*output)
		fmt.Fprintf(os.Stderr, "ERROR: close formatted model: %v\n", err)
		return 1
	}
	return 0
}

func writeCLIJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
