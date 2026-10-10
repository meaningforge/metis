package regression

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
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

func TestSuiteRejectsUnsupportedFilterNumberSyntax(t *testing.T) {
	for _, literal := range []string{"1e99999", ".nan", "012", "0x10", "1_000", "+12"} {
		input := strings.Replace(validSuite, "model: sales", "model: sales\n        filters: {kind: filter, filter: {field: amount, operator: eq, value: "+literal+"}}", 1)
		if _, _, err := ParseSuite([]byte(input)); err == nil {
			t.Errorf("accepted unsupported YAML filter %s", literal)
		}
	}
}

func TestSuitePreservesExactFilterValues(t *testing.T) {
	for _, literal := range []string{"9007199254740993", "1000000000000000100", "0.10000000000000000001", "1e3", "1e400", "1e-400", "[0.1, 9007199254740993]", "true", "null", "\"9007199254740993\""} {
		input := strings.Replace(validSuite, "model: sales", "model: sales\n        filters: {kind: filter, filter: {field: amount, operator: eq, value: "+literal+"}}", 1)
		suite, _, err := ParseSuite([]byte(input))
		if err != nil {
			t.Errorf("rejected %s: %v", literal, err)
			continue
		}
		if _, err := suite.Cases[0].Request.Query.semanticQuery(); err != nil {
			t.Errorf("conversion %s: %v", literal, err)
		}
	}
	for _, literal := range []string{"9007199254740993", "0.10000000000000000001", "[1, 9007199254740993]"} {
		input := `{"schema_version":1,"project":"demo","cases":[{"id":"exact","operation":"compile_sql","request":{"query":{"project":"demo","model":"sales","metrics":[{"name":"total_revenue"}],"filters":{"kind":"filter","filter":{"field":"amount","operator":"eq","value":` + literal + `}}}},"expect":{"outcome":"success","output_schema":{"columns":[{"name":"total_revenue","kind":"metric","datatype":"Decimal"}]}}}]}`
		if _, _, err := ParseSuite([]byte(input)); err != nil {
			t.Errorf("exact JSON filter %s: %v", literal, err)
		}
	}
	input := strings.Replace(validSuite, "model: sales", "model: sales\n        filters: {kind: filter, filter: {field: amount, operator: eq, value: 0.10000000000000000001}}", 1)
	suite, _, err := ParseSuite([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery, err := suite.Cases[0].Request.Query.semanticQuery()
	if err != nil {
		t.Fatal(err)
	}
	if got := semanticQuery.Filters.Leaves()[0].Value; got != json.Number("0.10000000000000000001") {
		t.Fatalf("YAML numeric filter = %#v", got)
	}
	for _, value := range []any{int64(9007199254740993), uint64(18446744073709551615), []any{int64(9007199254740993)}} {
		if _, err := (QueryInput{Filters: &PredicateInput{Kind: "filter", Filter: &FilterInput{Field: "amount", Operator: query.FilterEQ, Value: value}}}).semanticQuery(); err != nil {
			t.Errorf("exact in-memory filter %#v: %v", value, err)
		}
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
