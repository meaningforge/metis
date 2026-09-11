//go:build !duckdb

package command

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/app/bootstrap"
)

func loadSemanticProjectRuntime(configPath, _ string, database string, _ ...*physicalQueryCapture) (*bootstrap.Runtime, error) {
	if strings.TrimSpace(database) != "" {
		return nil, fmt.Errorf("executable S2SBench semantic runtime requires the duckdb build flavor")
	}
	return bootstrap.LoadRuntime(configPath, bootstrap.WithLocalAllAccessProjectAuthorization())
}
