// Command coverage renders the canonical semantic-correctness corpus as an
// on-demand view. Executable scenario contracts remain the source of truth.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func main() {
	if _, err := os.Stdout.Write(render(scenarios.Core)); err != nil {
		fatalf("write coverage report: %v", err)
	}
}

func render(registry []scenarios.Scenario) []byte {
	ordered := append([]scenarios.Scenario(nil), registry...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Category != ordered[j].Category {
			return ordered[i].Category < ordered[j].Category
		}
		return ordered[i].Name < ordered[j].Name
	})

	compilerRequired := 0
	resultRequired := 0
	resultPending := 0
	for _, scenario := range ordered {
		if scenario.Verification.Compiler == scenarios.ContractRequired {
			compilerRequired++
		}
		switch scenario.Verification.Result {
		case scenarios.ContractRequired:
			resultRequired++
		case scenarios.ContractPending:
			resultPending++
		}
	}

	var report strings.Builder
	report.WriteString("# Semantic Correctness Corpus Coverage\n\n")
	report.WriteString("This view is rendered from the canonical shared scenario registry. A `required` contract is enforced by its corresponding test gate; `pending` is an explicit result-level evidence gap.\n\n")
	fmt.Fprintf(&report, "- Canonical scenarios: **%d**\n", len(ordered))
	fmt.Fprintf(&report, "- Compiler contracts required: **%d**\n", compilerRequired)
	fmt.Fprintf(&report, "- Result contracts required: **%d**\n", resultRequired)
	fmt.Fprintf(&report, "- Result contracts pending: **%d**\n\n", resultPending)
	report.WriteString("| Scenario | Category | Fixture | Capabilities | Compiler | Result | Columns | Comparison | Expected rows | Gap |\n")
	report.WriteString("| --- | --- | --- | --- | :---: | :---: | --- | :---: | ---: | --- |\n")
	for _, scenario := range ordered {
		capabilities := make([]string, len(scenario.Requires))
		for i, capability := range scenario.Requires {
			capabilities[i] = string(capability)
		}
		sort.Strings(capabilities)

		expectedRows := 0
		columns := "—"
		comparison := "—"
		if scenario.ExpectedResult != nil {
			expectedRows = len(scenario.ExpectedResult.Rows)
			columnLabels := make([]string, len(scenario.ExpectedResult.Columns))
			for i, column := range scenario.ExpectedResult.Columns {
				columnLabels[i] = fmt.Sprintf("`%s:%s`", column.Name, column.ValueKind)
			}
			columns = strings.Join(columnLabels, ", ")
			comparison = string(scenario.ExpectedResult.Comparison)
		}
		fmt.Fprintf(
			&report,
			"| `%s` | %s | `%s` | %s | %s | %s | %s | %s | %d | %s |\n",
			scenario.Name,
			scenario.Category,
			scenario.Fixture,
			strings.Join(capabilities, ", "),
			statusLabel(scenario.Verification.Compiler),
			statusLabel(scenario.Verification.Result),
			columns,
			comparison,
			expectedRows,
			escapeCell(scenario.Verification.Reason),
		)
	}

	return []byte(report.String())
}

func statusLabel(status scenarios.ContractStatus) string {
	switch status {
	case scenarios.ContractRequired:
		return "✅ required"
	case scenarios.ContractPending:
		return "🟡 pending"
	default:
		return "❓ " + escapeCell(string(status))
	}
}

func escapeCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "coverage: "+format+"\n", args...)
	os.Exit(1)
}
