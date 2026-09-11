package s2sbench

import (
	"context"
	"fmt"
)

// Collection is the complete raw evidence for both arms of one frozen v0 run.
// It deliberately contains no derived score: reports are deterministic views
// that can always be regenerated from these immutable artifacts.
type Collection struct {
	RawAssets ArmRunArtifact
	Metis     ArmRunArtifact
}

// Collect executes both benchmark arms under the one budget recorded in the
// manifest and converts each complete run into auditable raw evidence. Runner
// is the injection boundary: live collection supplies real external-agent
// runners, while deterministic tests may supply in-process runners. Collect
// itself chooses no model, agent, transport, or credentials.
func Collect(ctx context.Context, manifest RunManifest, raw Runner, rawExecution Execution, metis Runner, metisExecution Execution) (Collection, error) {
	return CollectObserved(ctx, manifest, raw, rawExecution, metis, metisExecution, nil)
}

func CollectObserved(ctx context.Context, manifest RunManifest, raw Runner, rawExecution Execution, metis Runner, metisExecution Execution, observer AttemptObserver) (Collection, error) {
	if err := manifest.ValidateForCollection(); err != nil {
		return Collection{}, err
	}
	if raw == nil || raw.Path() != PathRawAssets {
		return Collection{}, fmt.Errorf("raw-assets runner must report path %q", PathRawAssets)
	}
	if metis == nil || metis.Path() != PathMetis {
		return Collection{}, fmt.Errorf("Metis runner must report path %q", PathMetis)
	}
	selected, err := ScenariosForManifest(manifest)
	if err != nil {
		return Collection{}, err
	}

	if rawExecution == nil || metisExecution == nil {
		return Collection{}, fmt.Errorf("execution is required for both benchmark arms")
	}
	rawRunner := &executingRunner{runner: raw, execution: rawExecution}
	metisRunner := &executingRunner{runner: metis, execution: metisExecution}
	rawAttempts := make([]Attempt, 0, len(selected)*manifest.Budget.Repetitions)
	metisAttempts := make([]Attempt, 0, len(selected)*manifest.Budget.Repetitions)
	for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
		for _, scenario := range selected {
			rawAttempt, recorded, err := runObservedQuestion(ctx, rawRunner, manifest.Budget, scenario, repetition, observer)
			if recorded {
				rawAttempts = append(rawAttempts, rawAttempt)
			}
			if err != nil {
				return Collection{}, fmt.Errorf("collect %s arm: %w", PathRawAssets, err)
			}

			metisAttempt, recorded, err := runObservedQuestion(ctx, metisRunner, manifest.Budget, scenario, repetition, observer)
			if recorded {
				metisAttempts = append(metisAttempts, metisAttempt)
			}
			if err != nil {
				return Collection{}, fmt.Errorf("collect %s arm: %w", PathMetis, err)
			}
		}
	}
	rawArtifact, err := NewArmRunArtifact(manifest, PathRawAssets, rawAttempts)
	if err != nil {
		return Collection{}, fmt.Errorf("build %s artifact: %w", PathRawAssets, err)
	}

	metisArtifact, err := NewArmRunArtifact(manifest, PathMetis, metisAttempts)
	if err != nil {
		return Collection{}, fmt.Errorf("build %s artifact: %w", PathMetis, err)
	}

	return Collection{RawAssets: rawArtifact, Metis: metisArtifact}, nil
}

// CollectArmObserved collects one complete arm without requiring or launching
// the other arm. The manifest remains the shared comparison identity, so two
// independently collected artifacts can be compared later only when their
// frozen manifests are identical.
func CollectArmObserved(ctx context.Context, manifest RunManifest, path Path, arm Runner, execution Execution, observer AttemptObserver) (ArmRunArtifact, error) {
	return CollectArmObservedFrom(ctx, manifest, path, arm, execution, nil, observer)
}

// CollectArmObservedFrom resumes one arm from already journaled, independently
// scored attempts. Completed scenario/repetition identities are never invoked
// again; the final artifact is rebuilt and fully validated from old plus new
// evidence.
func CollectArmObservedFrom(ctx context.Context, manifest RunManifest, path Path, arm Runner, execution Execution, completed []Attempt, observer AttemptObserver) (ArmRunArtifact, error) {
	if err := manifest.ValidateForCollection(); err != nil {
		return ArmRunArtifact{}, err
	}
	if path != PathRawAssets && path != PathMetis {
		return ArmRunArtifact{}, fmt.Errorf("unknown benchmark path %q", path)
	}
	if arm == nil || arm.Path() != path {
		return ArmRunArtifact{}, fmt.Errorf("runner must report path %q", path)
	}
	if execution == nil {
		return ArmRunArtifact{}, fmt.Errorf("execution is required for %s arm", path)
	}
	selected, err := ScenariosForManifest(manifest)
	if err != nil {
		return ArmRunArtifact{}, err
	}
	runner := &executingRunner{runner: arm, execution: execution}
	type attemptKey struct {
		scenario   string
		repetition int
	}
	allowed := make(map[string]struct{}, len(selected))
	for _, scenario := range selected {
		allowed[scenario.Name] = struct{}{}
	}
	done := make(map[attemptKey]struct{}, len(completed))
	attempts := append(make([]Attempt, 0, len(selected)*manifest.Budget.Repetitions), completed...)
	for index, attempt := range completed {
		if attempt.Path != path {
			return ArmRunArtifact{}, fmt.Errorf("completed attempt %d path is %q, want %q", index, attempt.Path, path)
		}
		if _, ok := allowed[attempt.Scenario]; !ok || attempt.Repetition < 1 || attempt.Repetition > manifest.Budget.Repetitions {
			return ArmRunArtifact{}, fmt.Errorf("completed attempt %d identity %s repetition %d is outside the manifest", index, attempt.Scenario, attempt.Repetition)
		}
		key := attemptKey{scenario: attempt.Scenario, repetition: attempt.Repetition}
		if _, exists := done[key]; exists {
			return ArmRunArtifact{}, fmt.Errorf("completed attempts duplicate %s repetition %d", attempt.Scenario, attempt.Repetition)
		}
		done[key] = struct{}{}
	}
	for repetition := 1; repetition <= manifest.Budget.Repetitions; repetition++ {
		for _, scenario := range selected {
			if _, exists := done[attemptKey{scenario: scenario.Name, repetition: repetition}]; exists {
				continue
			}
			attempt, recorded, runErr := runObservedQuestion(ctx, runner, manifest.Budget, scenario, repetition, observer)
			if recorded {
				attempts = append(attempts, attempt)
			}
			if runErr != nil {
				return ArmRunArtifact{}, fmt.Errorf("collect %s arm: %w", path, runErr)
			}
		}
	}
	artifact, err := NewArmRunArtifact(manifest, path, attempts)
	if err != nil {
		return ArmRunArtifact{}, fmt.Errorf("build %s artifact: %w", path, err)
	}
	return artifact, nil
}
