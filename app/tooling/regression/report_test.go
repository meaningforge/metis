package regression

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReportOverwriteRequiresRecognizableReport(t *testing.T) {
	report := Report{SchemaVersion: 1, Mode: "compile", Status: "passed", Cases: []CaseReport{{ID: "ok", Status: "passed"}}}
	for _, format := range []string{"json", "junit"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report")
			write := WriteReport
			if format == "junit" {
				write = WriteJUnitReport
			}
			for _, content := range []string{"semantic_sources: {}", "schema_version: 1\ncases: []", `{"version":1,"projects":{}}`, "<model/>", ""} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := write(path, report, true); err == nil {
					t.Fatal("overwrote non-report")
				}
				got, _ := os.ReadFile(path)
				if !bytes.Equal(got, []byte(content)) {
					t.Fatal("modified non-report")
				}
			}
			// Use a new path; only a real report authorizes subsequent replacement.
			path = filepath.Join(t.TempDir(), "real-report")
			if err := write(path, report, false); err != nil {
				t.Fatal(err)
			}
			if err := write(path, report, true); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, append(data, []byte("\nnot-a-report")...), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := write(path, report, true); err == nil {
				t.Fatal("overwrote malformed report")
			}
		})
	}
}
