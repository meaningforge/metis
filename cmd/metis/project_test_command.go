package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/renderer/sql"
)

func testProject(args []string) int {
	fs := flag.NewFlagSet("metis project test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "", "test mode; only compile is currently supported")
	project := fs.String("project", "", "stable project ID")
	config := fs.String("config", "", "semantic project manifest")
	suitePath := fs.String("suite", "", "strict YAML or JSON test suite")
	dialect := fs.String("dialect", "", "explicit SQL dialect for offline compilation")
	output := fs.String("output", "", "private JSON report path")
	overwrite := fs.Bool("overwrite", false, "replace an existing report")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *mode != "compile" || strings.TrimSpace(*project) == "" || strings.TrimSpace(*config) == "" ||
		strings.TrimSpace(*suitePath) == "" || strings.TrimSpace(*dialect) == "" || strings.TrimSpace(*output) == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "metis project test: --mode compile, --project, --config, --suite, --dialect, and --output are required; no positional arguments are supported")
		return 2
	}
	suite, digest, err := regression.LoadSuite(*suitePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: invalid suite: %v\n", err)
		return 2
	}
	report, err := regression.RunCompile(context.Background(), suite, digest, regression.CompileOptions{
		Project: *project, Config: *config, Dialect: sql.SQLDialect(strings.ToUpper(*dialect)),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: invalid input: %v\n", err)
		return 2
	}
	if err := regression.WriteReport(*output, report, *overwrite); err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: write report: %v\n", err)
		return 2
	}
	fmt.Fprintf(os.Stdout, "project compile suite: %s (%d passed, %d failed, %d not run); report: %s\n", report.Status, report.Passed, report.Failed, report.NotRun, *output)
	if report.Status != "passed" {
		return 1
	}
	return 0
}
