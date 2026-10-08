package command

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRoot constructs the complete public a2sbench command tree.
func NewRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "a2sbench",
		Short: "A2SBench: Agent-to-SQL benchmarks for Metis Core",
		Long: "A2SBench (Agent-to-SQL Benchmark) evaluates how agents use Metis Core to complete analytical tasks.\n" +
			"It owns experiment budgets, attempts, evidence, scoring and reports, not semantic-layer services.\n" +
			"Metis MCP is the system under test; OKF assets/direct SQL are controlled baselines with independent result scoring.",
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
