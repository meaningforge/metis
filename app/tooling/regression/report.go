package regression

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteReport writes private JSON on the destination filesystem. By default an
// existing report is never replaced, including a symlink or directory.
func WriteReport(path string, report Report, overwrite bool) error {
	if path == "" {
		return fmt.Errorf("report output path is required")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("report output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".metis-project-test-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(tempPath, path)
	}
	if err := os.Link(tempPath, path); err != nil {
		return fmt.Errorf("create report without replacing existing output: %w", err)
	}
	return nil
}
