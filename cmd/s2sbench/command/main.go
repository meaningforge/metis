package command

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRoot constructs the complete public s2sbench command tree.
func NewRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "s2sbench",
		Short:         "Generate, run, and analyze S2SBench semantic-interface experiments",
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
