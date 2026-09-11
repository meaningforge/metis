package harness

import (
	"fmt"
	"reflect"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// CompareNormalizedResults compares two engine-normalized result sets under the
// canonical scenario ordering contract. It retains duplicate rows for unordered
// results and never compares physical database types or raw textual values.
func CompareNormalizedResults(comparison scenarios.ResultComparison, left, right scenarios.ResultSet) error {
	if !reflect.DeepEqual(left.Columns, right.Columns) {
		return fmt.Errorf("columns differ\nleft:  %#v\nright: %#v", left.Columns, right.Columns)
	}
	if err := validateRows(left.Columns, left.Rows); err != nil {
		return fmt.Errorf("invalid left rows: %w", err)
	}
	if err := validateRows(right.Columns, right.Rows); err != nil {
		return fmt.Errorf("invalid right rows: %w", err)
	}
	if comparison == "" {
		return fmt.Errorf("result comparison is required")
	}
	if !resultRowsEqual(comparison, left.Rows, right.Rows) {
		leftRows := encodedRows(left.Rows)
		rightRows := encodedRows(right.Rows)
		return fmt.Errorf("normalized rows differ (%s)\nleft:  %v\nright: %v", comparison, leftRows, rightRows)
	}
	return nil
}
