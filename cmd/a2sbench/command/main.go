package command

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRoot constructs the complete public a2sbench command tree.
func NewRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "a2sbench",
		Short: "A2SBench: Agent-to-SQL benchmark experiments",
		Long: "A2SBench (Agent-to-SQL Benchmark) evaluates agents against frozen questions and independent result expectations.\n" +
			"It owns experiment budgets, attempts, evidence, scoring and reports, not semantic-layer services.\n" +
			"Current interface adapters are OKF assets and Metis MCP; this is not a claim of arbitrary engine support.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(
		newGenCommand(stdout),
		newOKFGenCommand(stdout),
		newRunCommand(stdout),
		newAnalyzeCommand(stdout),
		newReportCommand(stdout),
		newAttributionCommand(stdout),
		newComparisonCommand("comparison-smoke", "Run the comparison smoke experiment", stdout),
		newComparisonCommand("comparison-run", "Run the comparison experiment", stdout),
		newStopCommand(stdout),
	)
	return root
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	return NewRoot(stdout, stderr)
}
