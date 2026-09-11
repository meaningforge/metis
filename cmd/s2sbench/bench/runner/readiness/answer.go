package readiness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/meaningforge/metis/renderer/sql"
)

type Answer struct {
	Status     string               `json:"status"`
	Dialect    string               `json:"dialect,omitempty"`
	SQL        string               `json:"sql,omitempty"`
	Parameters []sql.QueryParameter `json:"parameters,omitempty"`
	Reason     string               `json:"reason,omitempty"`
}

var refusalReasons = map[string]struct{}{
	"ambiguous_identity": {}, "ambiguous_relationship": {}, "unsupported_semantics": {},
	"missing_expression": {}, "missing_physical_field": {}, "insufficient_scope": {},
}

func DecodeAnswer(output string) (Answer, error) {
	return DecodeAnswerForDialect(output, DuckDBTarget.Dialect)
}

func DecodeAnswerForDialect(output, expectedDialect string) (Answer, error) {
	trimmed := strings.TrimSpace(output)
	if strings.Contains(trimmed, "```") {
		return Answer{}, fmt.Errorf("answer must be bare JSON without Markdown fences")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(trimmed))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var answer Answer
	if err := decoder.Decode(&answer); err != nil {
		return Answer{}, fmt.Errorf("decode answer JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Answer{}, fmt.Errorf("answer contains more than one JSON value")
		}
		return Answer{}, fmt.Errorf("decode trailing answer JSON: %w", err)
	}
	switch answer.Status {
	case "ready":
		if !strings.EqualFold(answer.Dialect, expectedDialect) {
			return Answer{}, fmt.Errorf("ready answer dialect %q, want %s", answer.Dialect, expectedDialect)
		}
		if strings.TrimSpace(answer.Reason) != "" {
			return Answer{}, fmt.Errorf("ready answer must not contain reason")
		}
		statement, err := ValidateReadOnlySQL(answer.SQL)
		if err != nil {
			return Answer{}, err
		}
		answer.SQL = statement
	case "not_ready":
		if strings.TrimSpace(answer.Dialect) != "" || strings.TrimSpace(answer.SQL) != "" || len(answer.Parameters) != 0 {
			return Answer{}, fmt.Errorf("not_ready answer must not contain dialect, sql, or parameters")
		}
		if _, ok := refusalReasons[answer.Reason]; !ok {
			return Answer{}, fmt.Errorf("unknown not_ready reason %q", answer.Reason)
		}
	default:
		return Answer{}, fmt.Errorf("unknown answer status %q", answer.Status)
	}
	return answer, nil
}

func ValidateReadOnlySQL(input string) (string, error) {
	statement := strings.TrimSpace(input)
	if statement == "" {
		return "", fmt.Errorf("ready answer SQL is empty")
	}
	statements, err := splitSQLStatements(statement)
	if err != nil {
		return "", err
	}
	if len(statements) != 1 {
		return "", fmt.Errorf("ready answer must contain exactly one SQL statement")
	}
	statement = strings.TrimSpace(statements[0])
	first := firstSQLKeyword(statement)
	if first != "select" && first != "with" {
		return "", fmt.Errorf("SQL must begin with SELECT or WITH, got %q", first)
	}
	for _, forbidden := range []string{"insert", "update", "delete", "drop", "alter", "create", "attach", "detach", "copy", "call", "pragma", "install", "load", "export", "import", "vacuum", "truncate", "merge"} {
		if containsSQLKeyword(statement, forbidden) {
			return "", fmt.Errorf("SQL contains forbidden mutation or environment keyword %q", forbidden)
		}
	}
	return statement, nil
}

func SQLFingerprint(statement string, params ...sql.QueryParameter) string {
	normalized := strings.Join(strings.Fields(statement), " ")
	if len(params) > 0 {
		encoded, _ := json.Marshal(params)
		normalized += "\x00" + string(encoded)
	}
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func splitSQLStatements(input string) ([]string, error) {
	var statements []string
	start := 0
	quote := rune(0)
	inLineComment := false
	inBlockComment := false
	runes := []rune(input)
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		if inLineComment {
			if char == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if char == '*' && next == '/' {
				inBlockComment = false
				index++
			}
			continue
		}
		if quote != 0 {
			if char == quote {
				if next == quote {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		if char == '-' && next == '-' {
			inLineComment = true
			index++
			continue
		}
		if char == '/' && next == '*' {
			inBlockComment = true
			index++
			continue
		}
		if char == '\'' || char == '"' || char == '`' {
			quote = char
			continue
		}
		if char == ';' {
			part := strings.TrimSpace(string(runes[start:index]))
			if part != "" {
				statements = append(statements, part)
			}
			start = index + 1
		}
	}
	if quote != 0 || inBlockComment {
		return nil, fmt.Errorf("SQL contains an unterminated quote or block comment")
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		statements = append(statements, tail)
	}
	return statements, nil
}

func firstSQLKeyword(statement string) string {
	for _, token := range sqlTokens(statement) {
		return token
	}
	return ""
}

func containsSQLKeyword(statement, wanted string) bool {
	for _, token := range sqlTokens(statement) {
		if token == wanted {
			return true
		}
	}
	return false
}

func sqlTokens(input string) []string {
	var tokens []string
	runes := []rune(input)
	for index := 0; index < len(runes); {
		char := runes[index]
		if char == '-' && index+1 < len(runes) && runes[index+1] == '-' {
			index += 2
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			continue
		}
		if char == '/' && index+1 < len(runes) && runes[index+1] == '*' {
			index += 2
			for index+1 < len(runes) && !(runes[index] == '*' && runes[index+1] == '/') {
				index++
			}
			index += 2
			continue
		}
		if unicode.IsSpace(char) || strings.ContainsRune("(),.;+-*/%=<>![]", char) {
			index++
			continue
		}
		if char == '\'' || char == '"' || char == '`' {
			quote := char
			index++
			for index < len(runes) {
				if runes[index] == quote {
					if index+1 < len(runes) && runes[index+1] == quote {
						index += 2
						continue
					}
					index++
					break
				}
				index++
			}
			continue
		}
		start := index
		for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index]) || runes[index] == '_') {
			index++
		}
		if start == index {
			index++
			continue
		}
		tokens = append(tokens, strings.ToLower(string(runes[start:index])))
	}
	return tokens
}
