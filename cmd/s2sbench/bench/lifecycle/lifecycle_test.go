//go:build darwin || linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecycleUnreadyHelper(t *testing.T) {
	output := os.Getenv("S2SBENCH_UNREADY_HELPER_OUTPUT")
	if output == "" {
		return
	}
	lease, err := BeginPreparedCompatible(output, false, "unready-helper")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	time.Sleep(10 * time.Second)
}

func TestLeasePersistsRestrictiveStableControlState(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result with spaces")
	lease, err := Begin(output, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := lease.Paths()
	if err := lease.SetCompatibility("sha256:frame"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Ready(); err != nil {
		t.Fatal(err)
	}
	manifest := lease.Manifest()
	if manifest.RunState != StateRunning || manifest.OutputDirectory != paths.Output || manifest.PID != os.Getpid() {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, path := range []string{paths.Manifest, paths.PID, paths.Lock, paths.Log} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	if _, err := Begin(output, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("concurrent fresh begin error = %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Lock); !os.IsNotExist(err) {
		t.Fatalf("lease survived close: %v", err)
	}
}

func TestResumeRejectsCompatibilityDrift(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result")
	lease, err := Begin(output, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.SetCompatibility("frame-a"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output+".partial", 0o700); err != nil {
		t.Fatal(err)
	}
	paths := lease.Paths()
	manifest, err := readManifest(paths.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.PID = 1 << 29
	manifest.PGID = manifest.PID
	if err := writeManifest(paths.Manifest, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginCompatible(output, true, "frame-b"); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("compatibility drift error = %v", err)
	}
	persisted, err := readManifest(paths.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Compatibility != "frame-a" || persisted.Attempt != manifest.Attempt {
		t.Fatalf("resume drift mutated persisted manifest: %+v", persisted)
	}
}

func TestResumeReconcilesRenameBeforeCompletionState(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result")
	paths, err := ResolvePaths(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureControlDirectory(paths.Control); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Output, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := validTestManifest(t, paths)
	manifest.RunState = StatePublishing
	if err := writeManifest(paths.Manifest, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := Begin(output, true); !errors.Is(err, ErrRecoveredCompletion) {
		t.Fatalf("resume reconciliation error = %v", err)
	}
	reconciled, err := readManifest(paths.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.RunState != StateCompleted {
		t.Fatalf("reconciled state = %q", reconciled.RunState)
	}
}

func TestStopAlreadyExitedIsIdempotent(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result")
	paths, err := ResolvePaths(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureControlDirectory(paths.Control); err != nil {
		t.Fatal(err)
	}
	manifest := validTestManifest(t, paths)
	manifest.PID = 1 << 29
	manifest.PGID = manifest.PID
	manifest.RunState = StateInterrupted
	if err := writeManifest(paths.Manifest, manifest); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(paths.PID, []byte("536870912\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopped, err := Stop(paths.PID, false, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.RunState != StateStopped {
		t.Fatalf("state = %q", stopped.RunState)
	}
	if _, err := Stop(paths.Output, false, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestStartupTimeoutKillsChildAndReleasesVerifiedLease(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result")
	t.Setenv("S2SBENCH_UNREADY_HELPER_OUTPUT", output)
	_, err := SpawnDetached(context.Background(), []string{"-test.run=^TestLifecycleUnreadyHelper$"}, output, false, 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("startup timeout error = %v", err)
	}
	paths, err := ResolvePaths(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Lock); !os.IsNotExist(err) {
		t.Fatalf("unready child lease survived cleanup: %v", err)
	}
	manifest, err := readManifest(paths.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RunState != StateInterrupted || processExists(manifest.PID) {
		t.Fatalf("unready child was not safely cleaned up: %+v", manifest)
	}
}

func validTestManifest(t *testing.T, paths Paths) Manifest {
	t.Helper()
	pid := os.Getpid()
	pgid, err := processGroupID(pid)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := executableIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	started, err := processStartIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	return Manifest{
		SchemaVersion: ManifestSchemaVersion, ArtifactSchema: ArtifactSchemaVersion,
		RunID: "test-run", PID: pid, PGID: pgid, StartedAt: time.Now().UTC(),
		ExecutableIdentity: executable, ProcessStartIdentity: started,
		OutputDirectory: paths.Output, RunState: StateInterrupted,
		RetryPolicy: "skip-completed-retry-incomplete", Attempt: 1,
	}
}
