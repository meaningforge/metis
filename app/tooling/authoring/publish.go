package authoring

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meaningforge/metis/app/service/source"
)

// InitProject is the offline CLI composition. No backend, secret resolver,
// deployment configuration, or execution runtime is constructed here.
func InitProject(ctx context.Context, catalogPath, mappingPath, output string) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	snapshot, err := LoadSnapshot(catalogPath)
	if err != nil {
		return Report{}, err
	}
	mapping, err := LoadMapping(mappingPath)
	if err != nil {
		return Report{}, err
	}
	candidate, err := Generate(ctx, snapshot, mapping)
	if err != nil {
		return Report{}, err
	}
	if err := WriteCandidate(ctx, output, candidate); err != nil {
		return Report{}, err
	}
	return candidate.Report, nil
}

func WriteCandidate(ctx context.Context, output string, candidate Candidate) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if output == "" {
		return invalid("output", "output directory is required")
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return invalid("output", "invalid output directory")
	}
	if _, err := os.Lstat(absolute); err == nil {
		return invalid("output", "output path already exists; use a fresh directory")
	} else if !os.IsNotExist(err) {
		return invalid("output", "cannot inspect output path")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return invalid("output", "parent directory must exist")
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	modelPath := "models/" + candidate.Report.Model + ".ossie.yaml"
	if err := validateName(candidate.Report.Model, "model"); err != nil {
		return err
	}
	expected := map[string]bool{"project.yaml": true, "authoring-report.json": true, "GETTING_STARTED.md": true, modelPath: true}
	if len(candidate.Files) != len(expected) {
		return invalid("candidate", "candidate must contain exactly the documented files")
	}
	paths := make([]string, 0, len(candidate.Files))
	for path := range candidate.Files {
		if !fs.ValidPath(path) || !expected[path] || strings.Contains(path, "\\") {
			return invalid("candidate", "invalid relative output path")
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	temporary, err := os.MkdirTemp(parent, ".metis-authoring-*")
	if err != nil {
		return invalid("output", "cannot create private staging directory")
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}() // only this invocation's private temporary directory
	if err := os.Mkdir(filepath.Join(temporary, "models"), 0o700); err != nil {
		return invalid("output", "cannot create staging model directory")
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(temporary, filepath.FromSlash(path)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return invalid("output", "cannot create temporary file")
		}
		_, writeErr := f.Write(candidate.Files[path])
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return invalid("output", "cannot finish temporary file")
		}
	}
	// Validate the actual temporary project through the same file loader as users.
	loaded, err := source.LoadProject(candidate.Report.Project, filepath.Join(temporary, "project.yaml"))
	if err != nil || loaded.Bundle.ContentDigest != candidate.Report.Validation.ContentDigest {
		return finding("AUTHORING_CANDIDATE_INVALID", "candidate", "temporary project differs from validated source evidence")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := renameExclusive(temporary, absolute); err != nil {
		return invalid("output", "cannot publish candidate without replacing an existing path")
	}
	published = true
	return nil
}
