package s2sbench

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

// Verdict is what the oracle decides about one answer.
type Verdict string

const (
	// VerdictCorrect: the answer carries the same values as the answer key.
	VerdictCorrect Verdict = "correct"

	// VerdictWrong: the answer ran and returned something else. This is the
	// verdict the benchmark exists to count. A wrong number that arrives
	// without complaint is the failure Metis claims to remove, and it is
	// deliberately not merged with VerdictFailed -- an arm that refuses is
	// behaving differently from an arm that confidently misreports, and
	// collapsing them would hide the whole finding.
	VerdictWrong Verdict = "wrong"

	// VerdictFailed: no answer was produced -- the model gave up, the SQL did
	// not run, or compilation was refused.
	VerdictFailed Verdict = "failed"
)

// Judge compares one produced result set against a scenario's answer key.
//
// The comparison is by position, value kind, and canonical value. Column names
// are deliberately not compared, and that is the load-bearing decision in this
// file.
//
// The corpus oracle (harness.CompareNormalizedResults) requires ResultColumn
// equality, name included, which is right for conformance: Metis chooses those
// names and a change in them is a change in Metis. It is wrong here. Path A
// writes its own SQL and picks its own aliases, so requiring "total_revenue" to
// be called "revenue" would score presentation and report it as semantics --
// and it would score it in the direction that flatters Metis, since path B
// inherits the corpus names for free. A benchmark whose headline number is
// partly an alias check is a benchmark nobody should act on.
//
// What is still compared is everything that carries meaning: how many columns,
// in what order, of what kind, holding which values, with row order significant
// exactly when the scenario says it is. A model that returns the right numbers
// under different labels is correct. A model that returns them in the wrong
// order, or returns a count where a sum was asked for, is not.
func Judge(scenario scenarios.Scenario, actual scenarios.ResultSet) (Verdict, error) {
	expected := scenario.ExpectedResult
	if expected == nil {
		return VerdictFailed, fmt.Errorf("scenario %q has no answer key", scenario.Name)
	}
	if err := judgeShape(expected.Columns, actual.Columns); err != nil {
		return VerdictWrong, err
	}
	if err := judgeRows(expected.ResultSet, actual, expected.Comparison); err != nil {
		return VerdictWrong, err
	}
	return VerdictCorrect, nil
}

func judgeShape(expected, actual []scenarios.ResultColumn) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("answer has %d columns, want %d", len(actual), len(expected))
	}
	for i := range expected {
		if expected[i].ValueKind != actual[i].ValueKind {
			return fmt.Errorf("column %d is %q, want %q (name %q is not compared)",
				i, actual[i].ValueKind, expected[i].ValueKind, actual[i].Name)
		}
	}
	return nil
}

func judgeRows(expected, actual scenarios.ResultSet, comparison scenarios.ResultComparison) error {
	want, err := validateRows(expected)
	if err != nil {
		return fmt.Errorf("answer key: %w", err)
	}
	got, err := validateRows(actual)
	if err != nil {
		return fmt.Errorf("answer: %w", err)
	}

	switch comparison {
	case scenarios.ResultUnordered:
		return judgeUnorderedRows(want, got)
	case scenarios.ResultOrdered:
		// Row sequence is part of the contract for these scenarios.
	case "":
		return fmt.Errorf("scenario declares no result comparison mode")
	default:
		return fmt.Errorf("unknown comparison mode %q", comparison)
	}

	if len(want) != len(got) {
		return fmt.Errorf("answer has %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if err := judgeRow(want[i], got[i]); err != nil {
			return fmt.Errorf("row %d is [%s], want [%s] (%s comparison): %w", i, encodeRow(got[i]), encodeRow(want[i]), comparison, err)
		}
	}
	return nil
}

func judgeUnorderedRows(expected, actual []scenarios.ResultRow) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("answer has %d rows, want %d", len(actual), len(expected))
	}
	matched := make([]bool, len(actual))
	for expectedIndex, want := range expected {
		found := false
		for actualIndex, got := range actual {
			if matched[actualIndex] || judgeRow(want, got) != nil {
				continue
			}
			matched[actualIndex] = true
			found = true
			break
		}
		if !found {
			return fmt.Errorf("no answer row matches expected row %d [%s]", expectedIndex, encodeRow(want))
		}
	}
	return nil
}

func validateRows(set scenarios.ResultSet) ([]scenarios.ResultRow, error) {
	out := make([]scenarios.ResultRow, 0, len(set.Rows))
	for rowIndex, row := range set.Rows {
		if len(row) != len(set.Columns) {
			return nil, fmt.Errorf("row %d has %d values, want %d", rowIndex, len(row), len(set.Columns))
		}
		for columnIndex, value := range row {
			if value.ValueKind != set.Columns[columnIndex].ValueKind {
				return nil, fmt.Errorf("row %d value %d is %q, want %q",
					rowIndex, columnIndex, value.ValueKind, set.Columns[columnIndex].ValueKind)
			}
		}
		out = append(out, append(scenarios.ResultRow(nil), row...))
	}
	return out, nil
}

func judgeRow(expected, actual scenarios.ResultRow) error {
	for column := range expected {
		want, got := expected[column], actual[column]
		if want.ValueKind != got.ValueKind || want.Null != got.Null {
			return fmt.Errorf("column %d type/null differs", column)
		}
		if want.Null {
			continue
		}
		if want.ValueKind == scenarios.ResultNumber {
			if !scenarios.NumbersWithinTolerance(want.Canonical, got.Canonical) {
				return fmt.Errorf("column %d numeric value differs by more than %s", column, scenarios.NumberComparisonTolerance)
			}
			continue
		}
		if want.Canonical != got.Canonical {
			return fmt.Errorf("column %d value differs", column)
		}
	}
	return nil
}

func encodeRow(row scenarios.ResultRow) string {
	fields := make([]string, 0, len(row))
	for _, value := range row {
		if value.Null {
			fields = append(fields, "\x00null")
			continue
		}
		fields = append(fields, string(value.ValueKind)+":"+value.Canonical)
	}
	return strings.Join(fields, "|")
}
