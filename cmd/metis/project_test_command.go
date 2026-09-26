package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/meaningforge/metis/app/tooling/regression"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/renderer/sql"
)

func testProject(args []string) int {
	fs := flag.NewFlagSet("metis project test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "", "test mode: compile or runtime")
	project := fs.String("project", "", "stable project ID")
	config := fs.String("config", "", "project manifest (compile) or deployment config (runtime)")
	suitePath := fs.String("suite", "", "strict YAML or JSON test suite")
	dialect := fs.String("dialect", "", "explicit SQL dialect for offline compilation")
	output := fs.String("output", "", "private JSON report path")
	junitOutput := fs.String("junit-output", "", "optional private JUnit XML report path")
	overwrite := fs.Bool("overwrite", false, "replace an existing report")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if (*mode != "compile" && *mode != "runtime") || strings.TrimSpace(*project) == "" || strings.TrimSpace(*config) == "" ||
		strings.TrimSpace(*suitePath) == "" || (*mode == "compile" && strings.TrimSpace(*dialect) == "") || (*mode == "runtime" && *dialect != "") || strings.TrimSpace(*output) == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "metis project test: --mode compile|runtime, --project, --config, --suite, and --output are required; --dialect is required only for compile; no positional arguments")
		return 2
	}
	jsonPath, jsonPathErr := canonicalReportPath(*output)
	xmlPath, xmlPathErr := canonicalReportPath(*junitOutput)
	if jsonPathErr != nil || (*junitOutput != "" && xmlPathErr != nil) {
		fmt.Fprintln(os.Stderr, "metis project test: output parent directory must exist")
		return 2
	}
	if *junitOutput != "" && jsonPath == xmlPath {
		fmt.Fprintln(os.Stderr, "metis project test: JSON and JUnit output paths must differ")
		return 2
	}
	for _, path := range []string{*output, *junitOutput} {
		if path == "" {
			continue
		}
		if info, err := os.Lstat(path); err == nil && (!*overwrite || !info.Mode().IsRegular()) {
			fmt.Fprintln(os.Stderr, "metis project test: output exists or is not a regular file:", path)
			return 2
		} else if err != nil && !os.IsNotExist(err) {
			return 2
		}
	}
	suite, digest, err := regression.LoadSuite(*suitePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: invalid suite: %v\n", err)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var report regression.Report
	if *mode == "compile" {
		report, err = regression.RunCompile(ctx, suite, digest, regression.CompileOptions{
			Project: *project, Config: *config, Dialect: sql.SQLDialect(strings.ToUpper(*dialect)),
		})
	} else {
		backends, backendErr := defaultBackends()
		if backendErr != nil {
			fmt.Fprintln(os.Stderr, "metis project test: backend assembly failed")
			return 2
		}
		report, err = regression.RunRuntime(ctx, suite, digest, regression.RuntimeOptions{
			Project: *project, Config: *config, Backends: backends, SecretResolver: runner.NewEnvSecretResolver(),
		})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: invalid input: %v\n", err)
		return 2
	}
	if err := regression.WriteReport(*output, report, *overwrite); err != nil {
		fmt.Fprintf(os.Stderr, "metis project test: write report: %v\n", err)
		return 2
	}
	if *junitOutput != "" {
		if err := regression.WriteJUnitReport(*junitOutput, report, *overwrite); err != nil {
			fmt.Fprintf(os.Stderr, "metis project test: write JUnit report: %v\n", err)
			return 2
		}
	}
	fmt.Fprintf(os.Stdout, "project %s suite: %s (%d passed, %d failed, %d not run); report: %s\n", *mode, report.Status, report.Passed, report.Failed, report.NotRun, *output)
	if report.Status != "passed" {
		return 1
	}
	return 0
}

func canonicalReportPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}
