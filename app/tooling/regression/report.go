package regression

import (
	"encoding/json"
	"encoding/xml"
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
	return writePrivateReport(path, data, overwrite)
}

// WriteJUnitReport reports incomplete cases as errors, never successful skips.
// Like JSON reports it excludes SQL, credentials, and expected/actual row values.
func WriteJUnitReport(path string, report Report, overwrite bool) error {
	type issue struct {
		Message string `xml:"message,attr"`
	}
	type testCase struct {
		Name    string `xml:"name,attr"`
		Failure *issue `xml:"failure,omitempty"`
		Error   *issue `xml:"error,omitempty"`
	}
	suite := struct {
		XMLName  xml.Name   `xml:"testsuite"`
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Errors   int        `xml:"errors,attr"`
		Cases    []testCase `xml:"testcase"`
	}{Name: "metis.project." + report.Mode, Tests: len(report.Cases)}
	for _, c := range report.Cases {
		entry := testCase{Name: c.ID}
		switch c.Status {
		case "passed":
		case "failed":
			entry.Failure = &issue{Message: c.Category}
			suite.Failures++
		default:
			entry.Error = &issue{Message: c.Category}
			suite.Errors++
		}
		suite.Cases = append(suite.Cases, entry)
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateReport(path, append([]byte(xml.Header), append(data, '\n')...), overwrite)
}

func writePrivateReport(path string, data []byte, overwrite bool) error {
	if path == "" {
		return fmt.Errorf("report output path is required")
	}
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
