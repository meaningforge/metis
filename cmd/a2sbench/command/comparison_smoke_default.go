//go:build !duckdb

package command

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newComparisonCommand(use, short string, _ io.Writer) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("comparison experiment requires CGO_ENABLED=1 and the duckdb build tag")
		},
	}
}
