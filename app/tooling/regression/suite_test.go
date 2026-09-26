package regression

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSuite = `schema_version: 1
project: demo
cases:
  - id: revenue_by_region
    operation: compile_sql
    request:
      query:
        project: demo
        model: sales
        metrics: [{name: total_revenue}]
        dimensions: [{name: region}]
    expect:
      outcome: success
      output_schema:
        columns:
          - {name: region, kind: dimension, datatype: String}
          - {name: total_revenue, kind: metric, datatype: Decimal}
      warnings: []
`

func TestParseSuiteAcceptsStrictYAMLAndJSON(t *testing.T) {
	for name, input := range map[string]string{
		"yaml": validSuite,
		"json": `{"schema_version":1,"project":"demo","cases":[{"id":"revenue_by_region","operation":"compile_sql","request":{"query":{"project":"demo","model":"sales","metrics":[{"name":"total_revenue"}]}},"expect":{"outcome":"success","output_schema":{"columns":[{"name":"total_revenue","kind":"metric","datatype":"Decimal"}]}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			suite, digest, err := ParseSuite([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if suite.Project != "demo" || len(suite.Cases) != 1 || len(digest) != 64 {
				t.Fatalf("suite = %#v, digest = %q", suite, digest)
			}
		})
	}
}

func TestParseSuiteRejectsAmbiguousOrUnsupportedInput(t *testing.T) {
	tests := map[string]string{
		"unknown nested query": strings.Replace(validSuite, "model: sales", "model: sales\n        execution_binding: warehouse", 1),
		"unknown expectation":  strings.Replace(validSuite, "warnings: []", "warnings: []\n      typo: true", 1),
		"duplicate key":        strings.Replace(validSuite, "project: demo\ncases:", "project: demo\nproject: other\ncases:", 1),
		"duplicate ID":         validSuite + strings.SplitN(validSuite, "cases:\n", 2)[1],
		"runtime operation":    strings.Replace(validSuite, "compile_sql", "query_metrics", 1),
		"policy denial":        strings.Replace(validSuite, "outcome: success", "outcome: policy_denied", 1),
		"empty assertion":      strings.Replace(validSuite, "      output_schema:\n        columns:\n          - {name: region, kind: dimension, datatype: String}\n          - {name: total_revenue, kind: metric, datatype: Decimal}\n", "", 1),
		"multiple documents":   validSuite + "---\nproject: other\n",
		"YAML alias":           strings.Replace(validSuite, "project: demo", "project: &id demo", 1) + "\nunused: *id\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseSuite([]byte(input)); err == nil {
				t.Fatal("accepted invalid suite")
			}
		})
	}
}

func TestLoadSuiteRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suite.yaml")
	if err := os.WriteFile(path, []byte(validSuite+strings.Repeat(" ", MaxSuiteBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSuite(path); err == nil {
		t.Fatal("accepted oversized suite")
	}
}
