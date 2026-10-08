//go:build !duckdb

package command

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newAttributionCommand(_ io.Writer) *cobra.Command {
	return &cobra.Command{
		Use: "attribution-run", Short: "Run the attribution experiment", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("production attribution experiment requires CGO_ENABLED=1 and the duckdb build tag")
		},
	}
}
