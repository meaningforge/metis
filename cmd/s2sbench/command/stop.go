package command

import (
	"fmt"
	"io"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/lifecycle"
	"github.com/spf13/cobra"
)

func newStopCommand(stdout io.Writer) *cobra.Command {
	var force bool
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "stop [--force] <output-path-or-pid-file>",
		Short: "Safely stop a verified detached S2SBench process group",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			manifest, err := lifecycle.Stop(args[0], force, timeout)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "S2SBench run %s is stopped (PID %d, PGID %d).\n", manifest.RunID, manifest.PID, manifest.PGID)
			return nil
		},
	}
	command.Flags().BoolVar(&force, "force", false, "send KILL instead of TERM to the verified process group")
	command.Flags().DurationVar(&timeout, "timeout", lifecycle.DefaultStopTimeout, "maximum time to wait for confirmed termination")
	return command
}
