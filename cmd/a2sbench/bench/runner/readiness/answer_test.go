package readiness

import (
	"fmt"
	"testing"
)

func TestDecodeAnswerStrictEnvelope(t *testing.T) {
	answer, err := DecodeAnswer("{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 'a;b' AS value\"}")
	if err != nil {
		t.Fatal(err)
	}
	if answer.SQL != "SELECT 'a;b' AS value" {
		t.Fatalf("SQL = %q", answer.SQL)
	}
	for name, output := range map[string]string{
		"prose":              "answer: {\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 1\"}",
		"fenced":             "```json\n{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 1\"}\n```",
		"missing end fence":  "```json\n{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 1\"}",
		"unknown field":      "{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 1\",\"extra\":true}",
		"multiple statement": "{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"SELECT 1; SELECT 2\"}",
		"mutation":           "{\"status\":\"ready\",\"dialect\":\"duckdb\",\"sql\":\"DELETE FROM x\"}",
		"wrong dialect":      "{\"status\":\"ready\",\"dialect\":\"postgres\",\"sql\":\"SELECT 1\"}",
		"unknown refusal":    "{\"status\":\"not_ready\",\"reason\":\"guess\"}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeAnswer(output); err == nil {
				t.Fatal("expected strict decoder rejection")
			}
		})
	}
}

func TestDecodeAnswerPreservesPhysicalQueryParameters(t *testing.T) {
	answer, err := DecodeAnswer(`{"status":"ready","dialect":"DUCKDB","sql":"SELECT ? AS value","parameters":[{"value":"O'Reilly"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if answer.SQL != "SELECT ? AS value" || len(answer.Parameters) != 1 || answer.Parameters[0].Value != "O'Reilly" {
		t.Fatalf("parameterized SQL = %q", answer.SQL)
	}
}

func TestDecodeAnswerUsesManifestDialect(t *testing.T) {
	if _, err := DecodeAnswerForDialect(`{"status":"ready","dialect":"SNOWFLAKE","sql":"SELECT 1"}`, "SNOWFLAKE"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReadOnlySQLIgnoresQuotedAndCommentKeywords(t *testing.T) {
	if _, err := ValidateReadOnlySQL("SELECT 'delete', amount FROM orders -- update is text"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateReadOnlySQL("WITH x AS (SELECT 1) DELETE FROM orders"); err == nil {
		t.Fatal("expected mutation in CTE statement to be rejected")
	}
}

func TestParameterFingerprintsIncludeValues(t *testing.T) {
	a, err := DecodeAnswer(`{"status":"ready","dialect":"DUCKDB","sql":"SELECT ?","parameters":[{"value":9007199254740993}]}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeCompileToolResponse(`{"sql_render_result":{"dialect":"DUCKDB","sql":"SELECT ?","parameters":[{"value":9007199254740993}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a.Parameters[0].Value) != "9007199254740993" || fmt.Sprint(b.Parameters[0].Value) != "9007199254740993" {
		t.Fatalf("lost numeric precision: %#v, %#v", a, b)
	}
	if SQLFingerprint(a.SQL, a.Parameters...) != SQLFingerprint(b.SQL, b.Parameters...) {
		t.Fatal("equivalent query bindings have different fingerprints")
	}
	b.Parameters[0].Value = "different"
	if SQLFingerprint(a.SQL, a.Parameters...) == SQLFingerprint(b.SQL, b.Parameters...) {
		t.Fatal("different bindings have identical fingerprints")
	}
}
