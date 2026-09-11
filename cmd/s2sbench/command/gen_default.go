//go:build !duckdb

package command

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newGenCommand(_ io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "gen",
		Short: "Generate a reproducible S2SBench workload bundle",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("s2sbench gen requires the DuckDB build; run make s2sbench-build")
		},
	}
}
