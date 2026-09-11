package s2sbench

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ValidateForArtifact validates the structural frozen frame of an already
// collected current-schema manifest without requiring its historical prompt
// version to equal the prompt version used by new collections. Live collection
// continues to use ValidateForCollection and therefore remains version-strict.
func (m RunManifest) ValidateForArtifact() error {
	if strings.TrimSpace(m.PromptVersion) == "" {
		return fmt.Errorf("prompt version is required")
	}
	current := m
	current.PromptVersion = PromptVersion
	return current.ValidateForCollection()
}

// NewHistoricalArmRunArtifact reconstructs an artifact from persisted evidence
// produced by an older prompt contract that still uses the current manifest
// schema and frozen scenario/budget frame.
func NewHistoricalArmRunArtifact(manifest RunManifest, path Path, attempts []Attempt) (ArmRunArtifact, error) {
	if err := manifest.ValidateForArtifact(); err != nil {
		return ArmRunArtifact{}, err
	}
	historical := manifest
	current := manifest
	current.PromptVersion = PromptVersion
	artifact, err := NewArmRunArtifact(current, path, attempts)
	if err != nil {
		return ArmRunArtifact{}, err
	}
	artifact.Manifest = historical
	return artifact, nil
}

// RestoreAttemptRecord recovers the in-memory Attempt shape used by artifact
// validation from one compact incremental journal record. Raw prompts and
// transcripts are deliberately not persisted and therefore remain empty.
func RestoreAttemptRecord(record AttemptRecord, budget TaskBudget) (Attempt, error) {
	if strings.TrimSpace(record.Scenario) == "" || record.Repetition < 1 || record.Index < 1 {
		return Attempt{}, fmt.Errorf("persisted attempt identity is incomplete")
	}
	attempt := Attempt{
		Path:          record.Path,
		Scenario:      record.Scenario,
		Repetition:    record.Repetition,
		Index:         record.Index,
		Question:      record.Question,
		StartedAt:     record.StartedAt,
		DurationMS:    record.DurationMS,
		SQL:           record.SQL,
		SemanticQuery: record.SemanticQuery,
		ToolCalls:     record.ToolCalls,
		ContextTokens: record.ContextTokens,
		OutputTokens:  record.OutputTokens,
		ToolTrace:     append([]ToolCallEvidence(nil), record.ToolTrace...),
		Trace:         append([]AttemptEvidence(nil), record.Trace...),
		Result:        record.Result,
		Verdict:       record.Verdict,
		Reason:        record.Reason,
	}
	if strings.TrimSpace(record.Error) != "" {
		used := record.ToolCalls
		if len(record.Trace) > 0 {
			used = 0
			for _, evidence := range record.Trace {
				used += evidence.ToolCalls
			}
		}
		attempt.Err = restorePersistedAttemptError(record.Error, used, budget)
	}
	return attempt, nil
}

func restorePersistedAttemptError(message string, used int, budget TaskBudget) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	// Incremental JSONL intentionally stores a stable error string rather than Go
	// concrete types. Rehydrate the one typed error required by artifact budget
	// validation only when every persisted value agrees exactly.
	if used > budget.ToolCalls {
		typed := NewToolBudgetExceededError(used, budget.ToolCalls)
		if message == typed.Error() {
			return typed
		}
	}
	return errors.New(message)
}

// RestoreHistoricalCollection rebuilds both arm artifacts from a complete
// attempts.jsonl journal without invoking any Agent or query engine.
func RestoreHistoricalCollection(manifest RunManifest, records []AttemptRecord) (Collection, error) {
	if err := manifest.ValidateForArtifact(); err != nil {
		return Collection{}, fmt.Errorf("validate persisted manifest: %w", err)
	}
	raw := make([]Attempt, 0, len(records)/2)
	metis := make([]Attempt, 0, len(records)/2)
	for i, record := range records {
		attempt, err := RestoreAttemptRecord(record, manifest.Budget)
		if err != nil {
			return Collection{}, fmt.Errorf("restore attempt %d: %w", i, err)
		}
		switch attempt.Path {
		case PathRawAssets:
			raw = append(raw, attempt)
		case PathMetis:
			metis = append(metis, attempt)
		default:
			return Collection{}, fmt.Errorf("restore attempt %d has unknown path %q", i, attempt.Path)
		}
	}
	rawArtifact, err := NewHistoricalArmRunArtifact(manifest, PathRawAssets, raw)
	if err != nil {
		return Collection{}, fmt.Errorf("build raw-assets artifact: %w", err)
	}
	metisArtifact, err := NewHistoricalArmRunArtifact(manifest, PathMetis, metis)
	if err != nil {
		return Collection{}, fmt.Errorf("build metis artifact: %w", err)
	}
	return Collection{RawAssets: rawArtifact, Metis: metisArtifact}, nil
}

// BuildHistoricalV0Report derives a report for a recovered current-schema
// historical collection while preserving its original prompt version.
func BuildHistoricalV0Report(collection Collection) (V0Report, error) {
	if !reflect.DeepEqual(collection.RawAssets.Manifest, collection.Metis.Manifest) {
		return V0Report{}, fmt.Errorf("benchmark arms do not share the identical persisted manifest")
	}
	historical := collection.RawAssets.Manifest
	if err := historical.ValidateForArtifact(); err != nil {
		return V0Report{}, err
	}
	current := collection
	current.RawAssets.Manifest.PromptVersion = PromptVersion
	current.Metis.Manifest.PromptVersion = PromptVersion
	report, err := BuildV0Report(current)
	if err != nil {
		return V0Report{}, err
	}
	report.Manifest = historical
	return report, nil
}
