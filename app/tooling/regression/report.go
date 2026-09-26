package regression

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteReport writes private JSON on the destination filesystem. By default an
// existing report is never replaced, including a symlink or directory.
func WriteReport(path string, report Report, overwrite bool) error {
	if err := CheckReportOutput(path, "json", overwrite); err != nil {
		return err
	}
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
	if err := CheckReportOutput(path, "junit", overwrite); err != nil {
		return err
	}
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
		message := c.Category
		if c.Code != "" {
			message += ": " + c.Code
		}
		switch c.Status {
		case "passed":
		case "failed":
			entry.Failure = &issue{Message: message}
			suite.Failures++
		default:
			entry.Error = &issue{Message: message}
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

// CheckReportOutput is a preflight guard, also repeated by the writers. Explicit
// overwrite authorizes replacing an existing report, not arbitrary source files.
func CheckReportOutput(path, format string, overwrite bool) error {
	if path == "" {
		return fmt.Errorf("report output path is required")
	}
	if format != "json" && format != "junit" {
		return fmt.Errorf("unsupported report format")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !overwrite {
		return fmt.Errorf("report output already exists: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("report output is not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	// Bound identification separately from the much smaller input-suite limit.
	const maxExistingReportBytes = 16 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxExistingReportBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxExistingReportBytes {
		return fmt.Errorf("existing output is too large to identify safely")
	}
	valid := false
	if format == "json" {
		var report Report
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&report) == nil && report.SchemaVersion == 1 && (report.Mode == "compile" || report.Mode == "runtime") && (report.Status == "passed" || report.Status == "failed") {
			var extra any
			valid = decoder.Decode(&extra) == io.EOF
		}
	} else {
		var report struct {
			XMLName xml.Name `xml:"testsuite"`
			Name    string   `xml:"name,attr"`
		}
		decoder := xml.NewDecoder(bytes.NewReader(data))
		if decoder.Decode(&report) == nil && report.XMLName.Space == "" && (report.Name == "metis.project.compile" || report.Name == "metis.project.runtime") {
			valid = true
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					valid = false
					break
				}
				if whitespace, ok := token.(xml.CharData); !ok || len(bytes.TrimSpace(whitespace)) != 0 {
					valid = false
					break
				}
			}
		}
	}
	if !valid {
		return fmt.Errorf("refusing to overwrite a file that is not a Metis %s report: %s", format, path)
	}
	return nil
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
