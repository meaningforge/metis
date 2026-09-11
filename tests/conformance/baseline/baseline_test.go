package baseline_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// The compiled-SQL archive is the byte-stability gate for planner changes. Moving ownership of a semantic field from flat plan state or the
// DAG wrapper into SemanticPlan is an IR change; it is not permission
// to render different SQL. So a diff here is read as a SQL behavior change and
// needs its own approved correctness fix and recorded expectation, whichever
// migration phase surfaced it.
func TestCompiledSQLFingerprintsMatchTheArchivedBaseline(t *testing.T) {
	assertArchive(t, baseline.KindCompiledSQL,
		"Compiled SQL changed. This is not a migration detail: ownership\n"+
			"moving inside the IR must leave rendered SQL byte identical. Either the change is\n"+
			"an unintended lowering divergence and belongs fixed, or it is an independently\n"+
			"approved correctness fix, in which case record the new bytes with:\n  %s")
}

// SQL hashes detect parameter movement but do not show reviewers what moved.
// The baseline therefore freezes the exact wire evidence independently before the
// SQLPlan producer and renderers are cut over.
func TestCompiledParametersMatchTheArchivedBaseline(t *testing.T) {
	assertArchive(t, baseline.KindCompiledParameters,
		"Compiled parameter evidence changed. The parameter contract requires count, order, names,\n"+
			"values, and types to remain identical through SQLPlan migration. Fix an accidental\n"+
			"movement or document an independently approved contract change, then re-record with:\n  %s")
}

// The semantic-plan archive is held to the opposite contract. These hashes
// describe IR structure, so they are expected to move as nodes, requested
// outputs and the output contract become directly plan owned -- and a phase that
// claimed to move ownership without moving any of them would be the surprising
// result.
//
// It is archived anyway, and it fails on drift, because "expected to move" is
// not the same as "moves unobserved". The failure asks for a disposition: which
// fields moved into plan ownership, and why that is the movement the phase
// intended. Re-recording is the normal resolution here; for the compiled-SQL
// archive above it is the exception.
func TestSemanticPlanFingerprintsMatchTheArchivedBaseline(t *testing.T) {
	assertArchive(t, baseline.KindSemanticPlan,
		"Semantic-plan structure changed. This is expected when a planner change moves IR ownership\n"+
			"and it is not a compiled-SQL byte-stability failure -- that gate is asserted\n"+
			"separately above. Record which fields moved and why in the change's projection\n"+
			"disposition, then re-record with:\n  %s")
}

// Both archives are only evidence if the same tree produces the same hashes.
// Set-like plan fields are canonicalized rather than emitted in map order, and a
// projection that reintroduced nondeterminism would otherwise turn every later
// migration diff into noise that reviewers learn to re-record without reading.
func TestFingerprintsAreDeterministic(t *testing.T) {
	for _, kind := range baseline.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			first, err := baseline.Fingerprints(kind)
			if err != nil {
				t.Fatalf("compute %s fingerprints: %v", kind, err)
			}
			second, err := baseline.Fingerprints(kind)
			if err != nil {
				t.Fatalf("recompute %s fingerprints: %v", kind, err)
			}
			for _, line := range difference(first, second) {
				t.Errorf("fingerprint is not reproducible within one process: %s", line)
			}
		})
	}
}

// The archive is a baseline for the corpus, so it has to cover the corpus. A
// scenario or target added without re-recording would otherwise leave the
// migration comparing against evidence that never observed it.
func TestArchivesCoverEveryTargetAndScenario(t *testing.T) {
	var want []string
	for _, target := range evidence.CompilerTargets() {
		for _, scenario := range scenarios.Core {
			want = append(want, target.Dialect+"/"+scenario.Name)
		}
	}
	sort.Strings(want)

	for _, kind := range baseline.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			archived, err := loadArchive(kind)
			if err != nil {
				t.Fatalf("%v", err)
			}
			var have []string
			for key := range archived {
				have = append(have, key)
			}
			sort.Strings(have)

			if missing := difference(want, have); len(missing) != 0 {
				t.Errorf("%s is missing these target/scenario pairs:\n  %s\n\nRe-record with:\n  %s",
					baseline.Path(kind), strings.Join(missing, "\n  "), baseline.RegenerateCommand(kind))
			}
			if stale := difference(have, want); len(stale) != 0 {
				t.Errorf("%s records these target/scenario pairs, which the corpus no longer has:\n  %s\n\nRe-record with:\n  %s",
					baseline.Path(kind), strings.Join(stale, "\n  "), baseline.RegenerateCommand(kind))
			}
		})
	}
}

func assertArchive(t *testing.T, kind baseline.Kind, drift string) {
	t.Helper()

	archived, err := loadArchive(kind)
	if err != nil {
		t.Fatalf("%v", err)
	}
	lines, err := baseline.Fingerprints(kind)
	if err != nil {
		t.Fatalf("compute %s fingerprints: %v", kind, err)
	}

	var moved, added []string
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		key, value := split(line)
		seen[key] = true
		switch recorded, ok := archived[key]; {
		case !ok:
			added = append(added, fmt.Sprintf("%s %s", key, value))
		case recorded != value:
			moved = append(moved, fmt.Sprintf("%s\n    archived %s\n    current  %s", key, recorded, value))
		}
	}
	var removed []string
	for key := range archived {
		if !seen[key] {
			removed = append(removed, key)
		}
	}
	sort.Strings(moved)
	sort.Strings(added)
	sort.Strings(removed)

	if len(moved) == 0 && len(added) == 0 && len(removed) == 0 {
		return
	}
	var report strings.Builder
	if len(moved) != 0 {
		fmt.Fprintf(&report, "\n%d fingerprint(s) moved:\n  %s\n", len(moved), strings.Join(moved, "\n  "))
	}
	if len(added) != 0 {
		fmt.Fprintf(&report, "\n%d entr(ies) are not in %s:\n  %s\n", len(added), baseline.Path(kind), strings.Join(added, "\n  "))
	}
	if len(removed) != 0 {
		fmt.Fprintf(&report, "\n%d archived entr(ies) are no longer produced:\n  %s\n", len(removed), strings.Join(removed, "\n  "))
	}
	t.Errorf("%s\n%s", report.String(), fmt.Sprintf(drift, baseline.RegenerateCommand(kind)))
}

// loadArchive reads one archive keyed by target/scenario. Paths are
// repository relative because the archive is referenced from the regeneration
// command and from documentation, not only from this test.
func loadArchive(kind baseline.Kind) (map[string]string, error) {
	path := filepath.Join("..", "..", "..", filepath.FromSlash(baseline.Path(kind)))
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", baseline.Path(kind), err)
	}
	entries := map[string]string{}
	for number, line := range strings.Split(strings.TrimRight(string(contents), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value := split(line)
		if value == "" {
			return nil, fmt.Errorf("%s:%d: malformed archive entry %q", baseline.Path(kind), number+1, line)
		}
		if _, duplicate := entries[key]; duplicate {
			return nil, fmt.Errorf("%s:%d: duplicate archive entry for %s", baseline.Path(kind), number+1, key)
		}
		entries[key] = value
	}
	return entries, nil
}

// split separates the target/scenario key from its recorded value. The value is
// either a digest or an unsupported-capability list; both are compared as text
// so a scenario that stops being expressible on a target reads as a movement
// rather than as a silently absent line.
func split(line string) (key, value string) {
	key, value, _ = strings.Cut(line, " ")
	return key, value
}

// difference returns the members of left that are absent from right.
func difference(left, right []string) []string {
	index := make(map[string]struct{}, len(right))
	for _, value := range right {
		index[value] = struct{}{}
	}
	var out []string
	for _, value := range left {
		if _, ok := index[value]; !ok {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
