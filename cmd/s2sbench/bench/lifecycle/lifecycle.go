// Package lifecycle owns S2SBench run identity, single-writer leases, result
// publication state, detached startup, and safe process-group control.
package lifecycle

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ManifestSchemaVersion = 1
	ArtifactSchemaVersion = 1
	DefaultStartupTimeout = 30 * time.Second
	DefaultStopTimeout    = 5 * time.Second
)

type RunState string

const (
	StateInitializing RunState = "initializing"
	StateRunning      RunState = "running"
	StatePublishing   RunState = "publishing"
	StateCompleted    RunState = "completed"
	StateInterrupted  RunState = "interrupted"
	StateStopped      RunState = "stopped"
)

var ErrRecoveredCompletion = errors.New("completed result publication was recovered")

type Manifest struct {
	SchemaVersion        int       `json:"schema_version"`
	ArtifactSchema       int       `json:"artifact_schema_version"`
	RunID                string    `json:"run_id"`
	PID                  int       `json:"pid"`
	PGID                 int       `json:"pgid"`
	StartedAt            time.Time `json:"started_at"`
	ExecutableIdentity   string    `json:"executable_identity"`
	ProcessStartIdentity string    `json:"process_start_identity"`
	OutputDirectory      string    `json:"output_directory"`
	RunState             RunState  `json:"run_state"`
	Compatibility        string    `json:"compatibility,omitempty"`
	RetryPolicy          string    `json:"retry_policy"`
	Attempt              int       `json:"attempt"`
}

type Paths struct {
	Output   string
	Partial  string
	Control  string
	Manifest string
	PID      string
	Log      string
	Lock     string
}

func ResolvePaths(output string) (Paths, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return Paths{}, fmt.Errorf("output path is required")
	}
	abs, err := filepath.Abs(output)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve output path: %w", err)
	}
	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create output parent: %w", err)
	}
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return Paths{}, fmt.Errorf("canonicalize output parent: %w", err)
	}
	abs = filepath.Join(canonicalParent, filepath.Base(abs))
	control := abs + ".s2sbench"
	return Paths{
		Output: abs, Partial: abs + ".partial", Control: control,
		Manifest: filepath.Join(control, "manifest.json"),
		PID:      filepath.Join(control, "runner.pid"),
		Log:      filepath.Join(control, "console.log"),
		Lock:     filepath.Join(control, "run.lock"),
	}, nil
}

func CompatibilityDigest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode compatibility identity: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

type Lease struct {
	mu                    sync.Mutex
	paths                 Paths
	manifest              Manifest
	previousCompatibility string
	lock                  *os.File
	terminal              bool
}

func Begin(output string, resume bool) (*Lease, error) {
	return begin(output, resume, "", false)
}

// BeginCompatible acquires the single-writer lease and validates resume
// compatibility before replacing any persisted run metadata.
func BeginCompatible(output string, resume bool, compatibility string) (*Lease, error) {
	if strings.TrimSpace(compatibility) == "" {
		return nil, fmt.Errorf("run compatibility identity is required")
	}
	return begin(output, resume, compatibility, false)
}

// BeginPreparedCompatible is reserved for a detached child whose parent has
// just created the control directory and console log after a collision check.
func BeginPreparedCompatible(output string, resume bool, compatibility string) (*Lease, error) {
	if strings.TrimSpace(compatibility) == "" {
		return nil, fmt.Errorf("run compatibility identity is required")
	}
	return begin(output, resume, compatibility, true)
}

func begin(output string, resume bool, compatibility string, prepared bool) (*Lease, error) {
	paths, err := ResolvePaths(output)
	if err != nil {
		return nil, err
	}
	if !resume && !prepared {
		if _, err := os.Lstat(paths.Control); err == nil {
			return nil, fmt.Errorf("S2SBench control path %q already exists; refusing to overwrite it", paths.Control)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect S2SBench control path %q: %w", paths.Control, err)
		}
	}
	if err := ensureControlDirectory(paths.Control); err != nil {
		return nil, err
	}
	lock, err := acquireRunLock(paths)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = lock.Close()
			_ = os.Remove(paths.Lock)
		}
	}()
	logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("initialize console log: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return nil, fmt.Errorf("close console log: %w", err)
	}

	previous, previousErr := readManifest(paths.Manifest)
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return nil, fmt.Errorf("read run manifest: %w", previousErr)
	}
	if resume {
		if os.IsNotExist(previousErr) {
			return nil, fmt.Errorf("resume requires a valid run manifest at %q", paths.Manifest)
		}
		if err := validateManifest(previous, paths); err != nil {
			return nil, err
		}
		if recovered, err := reconcilePublication(paths, &previous); err != nil {
			return nil, err
		} else if recovered {
			return nil, ErrRecoveredCompletion
		}
		if previous.RunState == StateCompleted {
			return nil, fmt.Errorf("run %q is already completed", paths.Output)
		}
		if active, identityErr := verifiedActive(previous); active {
			return nil, fmt.Errorf("run %q is still active with PID %d", paths.Output, previous.PID)
		} else if identityErr != nil && processExists(previous.PID) {
			return nil, fmt.Errorf("cannot safely establish ownership of existing run: %w", identityErr)
		}
		if _, err := os.Stat(paths.Partial); err != nil {
			return nil, fmt.Errorf("resume requires incomplete result directory %q: %w", paths.Partial, err)
		}
	} else {
		if previousErr == nil {
			return nil, fmt.Errorf("control state %q already exists; use --resume for an incomplete run", paths.Control)
		}
		for _, candidate := range []string{paths.Output, paths.Partial} {
			if _, err := os.Lstat(candidate); err == nil {
				return nil, fmt.Errorf("S2SBench path %q already exists; refusing to overwrite it", candidate)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect S2SBench path %q: %w", candidate, err)
			}
		}
	}

	if resume && compatibility != "" && previous.Compatibility != compatibility {
		return nil, fmt.Errorf("resume configuration or workload differs from the persisted run")
	}

	pid := os.Getpid()
	pgid, err := processGroupID(pid)
	if err != nil {
		return nil, fmt.Errorf("resolve process group: %w", err)
	}
	executable, err := executableIdentity(pid)
	if err != nil {
		return nil, fmt.Errorf("resolve executable identity: %w", err)
	}
	started, err := processStartIdentity(pid)
	if err != nil {
		return nil, fmt.Errorf("resolve process start identity: %w", err)
	}
	runID, err := randomID()
	if err != nil {
		return nil, err
	}
	attempt := 1
	startedAt := time.Now().UTC()
	if resume {
		runID = previous.RunID
		attempt = previous.Attempt + 1
		startedAt = previous.StartedAt
	}
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion, ArtifactSchema: ArtifactSchemaVersion,
		RunID: runID, PID: pid, PGID: pgid, StartedAt: startedAt,
		ExecutableIdentity: executable, ProcessStartIdentity: started,
		OutputDirectory: paths.Output, RunState: StateInitializing,
		Compatibility: compatibility,
		RetryPolicy:   "skip-completed-retry-incomplete", Attempt: attempt,
	}
	if _, err := fmt.Fprintf(lock, "%s\n%d\n", runID, pid); err != nil {
		return nil, fmt.Errorf("write run lease: %w", err)
	}
	if err := lock.Sync(); err != nil {
		return nil, fmt.Errorf("sync run lease: %w", err)
	}
	lease := &Lease{paths: paths, manifest: manifest, lock: lock}
	if resume {
		lease.previousCompatibility = previous.Compatibility
	}
	if err := lease.persist(); err != nil {
		return nil, err
	}
	if err := atomicWrite(paths.PID, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write runner PID: %w", err)
	}
	ok = true
	return lease, nil
}

// acquireRunLock performs the only state inspection allowed before lease
// acquisition: verified recovery of a lock whose recorded process has exited.
// All fresh/resume validation happens after this returns an exclusive lock.
func acquireRunLock(paths Paths) (*os.File, error) {
	lock, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return lock, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("acquire run lease: %w", err)
	}
	previous, previousErr := readManifest(paths.Manifest)
	if err := recoverStaleLock(paths, previous, previousErr); err != nil {
		return nil, err
	}
	lock, err = os.OpenFile(paths.Lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("run %q already exists and is leased by another writer", paths.Output)
		}
		return nil, fmt.Errorf("acquire run lease after stale-lock recovery: %w", err)
	}
	return lock, nil
}

func (l *Lease) Paths() Paths { return l.paths }

func (l *Lease) Manifest() Manifest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.manifest
}

func (l *Lease) SetCompatibility(digest string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.TrimSpace(digest) == "" {
		return fmt.Errorf("run compatibility identity is required")
	}
	if l.previousCompatibility != "" && l.previousCompatibility != digest {
		return fmt.Errorf("resume configuration or workload differs from the persisted run")
	}
	l.manifest.Compatibility = digest
	return l.persistLocked()
}

func (l *Lease) Ready() error      { return l.transition(StateRunning) }
func (l *Lease) Publishing() error { return l.transition(StatePublishing) }

func (l *Lease) Complete() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.terminal {
		return nil
	}
	l.manifest.RunState = StateCompleted
	if err := l.persistLocked(); err != nil {
		return err
	}
	l.terminal = true
	return l.releaseLocked()
}

func (l *Lease) transition(state RunState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.terminal {
		return fmt.Errorf("run lease is already closed")
	}
	l.manifest.RunState = state
	return l.persistLocked()
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.terminal {
		return nil
	}
	if l.manifest.RunState != StatePublishing {
		l.manifest.RunState = StateInterrupted
		if err := l.persistLocked(); err != nil {
			return err
		}
	}
	l.terminal = true
	return l.releaseLocked()
}

func (l *Lease) persist() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.persistLocked()
}

func (l *Lease) persistLocked() error {
	body, err := json.MarshalIndent(l.manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lifecycle manifest: %w", err)
	}
	body = append(body, '\n')
	return atomicWrite(l.paths.Manifest, body, 0o600)
}

func (l *Lease) releaseLocked() error {
	var result error
	if l.lock != nil {
		if err := l.lock.Close(); err != nil {
			result = err
		}
		l.lock = nil
	}
	if err := os.Remove(l.paths.Lock); err != nil && !os.IsNotExist(err) {
		result = errors.Join(result, err)
	}
	return result
}

type Handshake struct {
	Status   string   `json:"status"`
	Message  string   `json:"message,omitempty"`
	Manifest Manifest `json:"manifest,omitempty"`
}

type Notifier struct{ file *os.File }

func NewNotifier(fd int) (*Notifier, error) {
	if fd < 3 {
		return nil, fmt.Errorf("invalid readiness descriptor %d", fd)
	}
	file := os.NewFile(uintptr(fd), "s2sbench-readiness")
	if file == nil {
		return nil, fmt.Errorf("readiness descriptor %d is unavailable", fd)
	}
	return &Notifier{file: file}, nil
}

func (n *Notifier) Ready(manifest Manifest) error {
	return n.send(Handshake{Status: "ready", Manifest: manifest})
}

func (n *Notifier) Failed(err error) error {
	message := "detached child initialization failed"
	if err != nil {
		message = err.Error()
	}
	return n.send(Handshake{Status: "error", Message: message})
}

func (n *Notifier) send(value Handshake) error {
	if n == nil || n.file == nil {
		return nil
	}
	err := json.NewEncoder(n.file).Encode(value)
	closeErr := n.file.Close()
	n.file = nil
	return errors.Join(err, closeErr)
}

func (n *Notifier) Close() error {
	if n == nil || n.file == nil {
		return nil
	}
	err := n.file.Close()
	n.file = nil
	return err
}

type DetachedInfo struct {
	Manifest Manifest
	Paths    Paths
}

func SpawnDetached(ctx context.Context, args []string, output string, resume bool, timeout time.Duration) (DetachedInfo, error) {
	paths, err := ResolvePaths(output)
	if err != nil {
		return DetachedInfo{}, err
	}
	if err := preflightDetached(paths, resume); err != nil {
		return DetachedInfo{}, err
	}
	if err := ensureControlDirectory(paths.Control); err != nil {
		return DetachedInfo{}, err
	}
	logFlags := os.O_CREATE | os.O_WRONLY
	if resume {
		logFlags |= os.O_APPEND
	} else {
		logFlags |= os.O_EXCL
	}
	logFile, err := os.OpenFile(paths.Log, logFlags, 0o600)
	if err != nil {
		return DetachedInfo{}, fmt.Errorf("open detached console log: %w", err)
	}
	defer logFile.Close()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return DetachedInfo{}, fmt.Errorf("open null input: %w", err)
	}
	defer devNull.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return DetachedInfo{}, fmt.Errorf("create readiness channel: %w", err)
	}
	defer reader.Close()
	executable, err := os.Executable()
	if err != nil {
		writer.Close()
		return DetachedInfo{}, fmt.Errorf("resolve current executable: %w", err)
	}
	command := exec.Command(executable, args...)
	command.Stdin, command.Stdout, command.Stderr = devNull, logFile, logFile
	command.ExtraFiles = []*os.File{writer}
	command.SysProcAttr = detachedProcessAttributes()
	if err := command.Start(); err != nil {
		writer.Close()
		return DetachedInfo{}, fmt.Errorf("start detached child: %w", err)
	}
	_ = writer.Close()

	result := make(chan struct {
		handshake Handshake
		err       error
	}, 1)
	go func() {
		var handshake Handshake
		line, readErr := bufio.NewReader(io.LimitReader(reader, 64<<10)).ReadBytes('\n')
		if readErr == nil {
			readErr = json.Unmarshal(line, &handshake)
		}
		result <- struct {
			handshake Handshake
			err       error
		}{handshake, readErr}
	}()
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var response Handshake
	select {
	case received := <-result:
		if received.err != nil {
			terminateUnready(command, paths)
			return DetachedInfo{}, fmt.Errorf("detached child closed readiness channel before startup: %w", received.err)
		}
		response = received.handshake
	case <-timer.C:
		terminateUnready(command, paths)
		return DetachedInfo{}, fmt.Errorf("detached startup timed out after %s", timeout)
	case <-ctx.Done():
		terminateUnready(command, paths)
		return DetachedInfo{}, fmt.Errorf("detached startup interrupted: %w", ctx.Err())
	}
	if response.Status != "ready" {
		terminateUnready(command, paths)
		if response.Message == "" {
			response.Message = "malformed readiness handshake"
		}
		return DetachedInfo{}, errors.New(response.Message)
	}
	if err := validateManifest(response.Manifest, paths); err != nil {
		terminateUnready(command, paths)
		return DetachedInfo{}, fmt.Errorf("invalid readiness handshake: %w", err)
	}
	go func() { _ = command.Wait() }()
	return DetachedInfo{Manifest: response.Manifest, Paths: paths}, nil
}

func preflightDetached(paths Paths, resume bool) error {
	manifest, manifestErr := readManifest(paths.Manifest)
	if resume {
		if manifestErr != nil {
			return fmt.Errorf("resume requires a valid run manifest at %q: %w", paths.Manifest, manifestErr)
		}
		if err := validateManifest(manifest, paths); err != nil {
			return err
		}
		if active, identityErr := verifiedActive(manifest); active {
			return fmt.Errorf("run %q is still active with PID %d", paths.Output, manifest.PID)
		} else if identityErr != nil && processExists(manifest.PID) {
			return fmt.Errorf("cannot safely establish ownership of existing run: %w", identityErr)
		}
		if recovered, err := reconcilePublication(paths, &manifest); err != nil {
			return err
		} else if recovered {
			return ErrRecoveredCompletion
		}
		if manifest.RunState == StateCompleted {
			return fmt.Errorf("run %q is already completed", paths.Output)
		}
		return nil
	}
	if manifestErr == nil {
		return fmt.Errorf("control state %q already exists; use --resume for an incomplete run", paths.Control)
	}
	if !os.IsNotExist(manifestErr) {
		return fmt.Errorf("inspect prior control state: %w", manifestErr)
	}
	if _, err := os.Lstat(paths.Control); err == nil {
		return fmt.Errorf("S2SBench control path %q already exists; refusing to overwrite it", paths.Control)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect S2SBench control path %q: %w", paths.Control, err)
	}
	for _, candidate := range []string{paths.Output, paths.Partial} {
		if _, err := os.Lstat(candidate); err == nil {
			return fmt.Errorf("S2SBench path %q already exists; refusing to overwrite it", candidate)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect S2SBench path %q: %w", candidate, err)
		}
	}
	return nil
}

func terminateUnready(command *exec.Cmd, paths Paths) {
	if command == nil || command.Process == nil {
		return
	}
	pid := command.Process.Pid
	if pgid, err := processGroupID(pid); err == nil && pgid == pid {
		_ = signalProcessGroup(pgid, true)
	} else {
		_ = command.Process.Kill()
	}
	_ = command.Wait()
	cleanupUnreadyLease(paths, pid)
}

func cleanupUnreadyLease(paths Paths, pid int) {
	if processExists(pid) {
		return
	}
	manifest, err := readManifest(paths.Manifest)
	if err != nil || manifest.PID != pid || manifest.OutputDirectory != paths.Output {
		return
	}
	body, err := os.ReadFile(paths.Lock)
	if err != nil {
		return
	}
	fields := strings.Fields(string(body))
	if len(fields) != 2 || fields[0] != manifest.RunID || fields[1] != strconv.Itoa(pid) {
		return
	}
	manifest.RunState = StateInterrupted
	if err := writeManifest(paths.Manifest, manifest); err != nil {
		return
	}
	_ = os.Remove(paths.Lock)
}

func Stop(target string, force bool, timeout time.Duration) (Manifest, error) {
	paths, err := pathsFromTarget(target)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateControlFiles(paths); err != nil {
		return Manifest{}, err
	}
	manifest, err := readManifest(paths.Manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("read run manifest: %w", err)
	}
	if err := validateManifest(manifest, paths); err != nil {
		return Manifest{}, err
	}
	pidBody, err := os.ReadFile(paths.PID)
	if err != nil {
		return Manifest{}, fmt.Errorf("read runner PID: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBody)))
	if err != nil || pid <= 0 || pid != manifest.PID {
		return Manifest{}, fmt.Errorf("runner PID metadata does not match the run manifest")
	}
	if !processExists(pid) {
		if manifest.RunState != StateCompleted {
			manifest.RunState = StateStopped
			_ = writeManifest(paths.Manifest, manifest)
		}
		return manifest, nil
	}
	active, err := verifiedActive(manifest)
	if err != nil || !active {
		if err == nil {
			err = fmt.Errorf("process is not active")
		}
		return Manifest{}, fmt.Errorf("refusing to signal unverified process identity: %w", err)
	}
	selfPGID, err := processGroupID(os.Getpid())
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve stop command process group: %w", err)
	}
	if manifest.PGID == selfPGID {
		return Manifest{}, fmt.Errorf("refusing to signal the stop command's own process group")
	}
	if err := signalProcessGroup(manifest.PGID, force); err != nil && processGroupExists(manifest.PGID) {
		return Manifest{}, err
	}
	if timeout <= 0 {
		timeout = DefaultStopTimeout
	}
	deadline := time.Now().Add(timeout)
	for processGroupExists(manifest.PGID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processGroupExists(manifest.PGID) {
		if force {
			return Manifest{}, fmt.Errorf("process group %d did not exit after KILL", manifest.PGID)
		}
		return Manifest{}, fmt.Errorf("process group %d did not exit after TERM; retry with --force", manifest.PGID)
	}
	manifest.RunState = StateStopped
	if err := writeManifest(paths.Manifest, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateControlFiles(paths Paths) error {
	control, err := os.Lstat(paths.Control)
	if err != nil {
		return fmt.Errorf("inspect control directory: %w", err)
	}
	if control.Mode()&os.ModeSymlink != 0 || !control.IsDir() {
		return fmt.Errorf("control path %q is not a real directory", paths.Control)
	}
	for _, path := range []string{paths.Manifest, paths.PID} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect control file %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("control file %q is not a regular file", path)
		}
	}
	return nil
}

func pathsFromTarget(target string) (Paths, error) {
	abs, err := filepath.Abs(strings.TrimSpace(target))
	if err != nil {
		return Paths{}, err
	}
	if filepath.Base(abs) == "runner.pid" && filepath.Base(filepath.Dir(abs)) != "." {
		control := filepath.Dir(abs)
		output := strings.TrimSuffix(control, ".s2sbench")
		if output == control {
			return Paths{}, fmt.Errorf("PID file must be inside an <output>.s2sbench control directory")
		}
		return ResolvePaths(output)
	}
	if strings.HasSuffix(abs, ".pid") {
		return Paths{}, fmt.Errorf("unrelated PID files are not valid stop targets")
	}
	return ResolvePaths(abs)
}

func verifiedActive(manifest Manifest) (bool, error) {
	if !processExists(manifest.PID) {
		return false, nil
	}
	pgid, err := processGroupID(manifest.PID)
	if err != nil {
		return false, err
	}
	if pgid != manifest.PGID || pgid != manifest.PID {
		return false, fmt.Errorf("PID-to-PGID identity changed")
	}
	started, err := processStartIdentity(manifest.PID)
	if err != nil {
		return false, err
	}
	if started != manifest.ProcessStartIdentity {
		return false, fmt.Errorf("process-start identity changed")
	}
	executable, err := executableIdentity(manifest.PID)
	if err != nil {
		return false, err
	}
	if executable != manifest.ExecutableIdentity {
		return false, fmt.Errorf("executable identity changed")
	}
	return true, nil
}

func validateManifest(manifest Manifest, paths Paths) error {
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.ArtifactSchema != ArtifactSchemaVersion {
		return fmt.Errorf("unsupported lifecycle manifest schema")
	}
	if manifest.RunID == "" || manifest.PID <= 0 || manifest.PGID <= 0 || manifest.ProcessStartIdentity == "" || manifest.ExecutableIdentity == "" {
		return fmt.Errorf("lifecycle manifest process identity is incomplete")
	}
	if manifest.OutputDirectory != paths.Output {
		return fmt.Errorf("lifecycle manifest is not associated with output %q", paths.Output)
	}
	return nil
}

func reconcilePublication(paths Paths, manifest *Manifest) (bool, error) {
	if manifest.RunState != StatePublishing {
		return false, nil
	}
	_, outputErr := os.Stat(paths.Output)
	_, partialErr := os.Stat(paths.Partial)
	switch {
	case outputErr == nil && os.IsNotExist(partialErr):
		manifest.RunState = StateCompleted
		if err := writeManifest(paths.Manifest, *manifest); err != nil {
			return false, err
		}
		return true, nil
	case os.IsNotExist(outputErr) && partialErr == nil:
		return false, nil
	default:
		return false, fmt.Errorf("cannot reconcile publishing state for %q", paths.Output)
	}
}

func recoverStaleLock(paths Paths, previous Manifest, previousErr error) error {
	info, err := os.Lstat(paths.Lock)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect run lease: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("run lease %q is not a regular file", paths.Lock)
	}
	if previousErr != nil {
		return fmt.Errorf("run lease exists without verifiable manifest; refusing stale-lock recovery")
	}
	body, err := os.ReadFile(paths.Lock)
	if err != nil {
		return fmt.Errorf("read run lease: %w", err)
	}
	fields := strings.Fields(string(body))
	if len(fields) != 2 || fields[0] != previous.RunID || fields[1] != strconv.Itoa(previous.PID) {
		return fmt.Errorf("run lease identity does not match the run manifest; refusing stale-lock recovery")
	}
	active, err := verifiedActive(previous)
	if active {
		return fmt.Errorf("run %q already exists and is leased by PID %d", paths.Output, previous.PID)
	}
	if err != nil && processExists(previous.PID) {
		return fmt.Errorf("run %q already exists and lease ownership is ambiguous: %w", paths.Output, err)
	}
	if err := os.Remove(paths.Lock); err != nil {
		return fmt.Errorf("remove verified stale run lease: %w", err)
	}
	return nil
}

func ensureControlDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("control path %q is not a real directory", path)
		}
		return os.Chmod(path, 0o700)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create control directory: %w", err)
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	file, err := os.Open(path)
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifest, fmt.Errorf("manifest has trailing data")
	}
	return manifest, nil
}

func writeManifest(path string, manifest Manifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(body, '\n'), 0o600)
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".s2sbench-write-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	if err != nil {
		return err
	}
	ok = true
	return nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate run identity: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
