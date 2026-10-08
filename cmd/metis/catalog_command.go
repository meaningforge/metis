package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/tooling/authoring"
	"github.com/meaningforge/metis/execution/runner"
)

func runCatalog(args []string) int {
	if len(args) == 0 || args[0] != "inspect" {
		fmt.Fprintln(os.Stderr, "usage: metis catalog inspect --config <metis.yaml> --project <name> --data-source <name> --relations <relations.json> --output <catalog.json>")
		return 2
	}
	fs := flag.NewFlagSet("metis catalog inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	config := fs.String("config", "", "deployment manifest (semantic project need not exist yet)")
	project := fs.String("project", "", "explicit Project identity")
	dataSource := fs.String("data-source", "", "DataSource applied to the Project")
	relations := fs.String("relations", "", "version 1 exact qualified relation selectors (JSON or YAML)")
	output := fs.String("output", "", "new catalog JSON file; existing paths are refused")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	for _, value := range []string{*config, *project, *dataSource, *relations, *output} {
		if strings.TrimSpace(value) == "" {
			fmt.Fprintln(os.Stderr, "metis catalog inspect: all five flags are required")
			return 2
		}
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "metis catalog inspect: no positional arguments")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// The local CLI is trusted OS/database-identity authoring. Remote embedders
	// must supply their own authorizer and physical metadata access policy.
	backends, err := defaultBackends()
	if err != nil {
		return catalogFailure(err)
	}
	execution, err := bootstrap.LoadCatalogRunner(ctx, *config, *project, *dataSource, bootstrap.LocalCatalogAccess, bootstrap.WithBackendRegistry(backends), bootstrap.WithLocalAllAccessProjectAuthorization(), bootstrap.WithSecretResolver(runner.NewEnvSecretResolver()))
	if err != nil {
		return catalogFailure(err)
	}
	selectors, err := authoring.LoadSelectors(*relations)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = execution.Close(cleanupCtx)
		cleanupCancel()
		return catalogFailure(err)
	}
	snapshot, inspectErr := authoring.InspectCatalog(ctx, execution, *project, *dataSource, selectors)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := execution.Close(cleanupCtx)
	cleanupCancel()
	if inspectErr != nil {
		return catalogFailure(inspectErr)
	}
	if closeErr != nil {
		return catalogFailure(closeErr)
	}
	if err := authoring.WriteSnapshot(ctx, *output, snapshot); err != nil {
		return catalogFailure(err)
	}
	fmt.Fprintln(os.Stdout, "catalog snapshot created; review evidence before metis semantic init")
	return 0
}

func catalogFailure(err error) int {
	var finding *authoring.Finding
	if errors.As(err, &finding) {
		fmt.Fprintln(os.Stderr, "metis catalog inspect:", finding)
		if finding.InvalidInput {
			return 2
		}
		return 1
	}
	var execution *runner.ExecutionError
	if errors.As(err, &execution) {
		fmt.Fprintln(os.Stderr, "metis catalog inspect:", execution)
		if execution.Code == runner.ExecutionInvalidInput {
			return 2
		}
		return 1
	}
	// Authorization, I/O, provider and context errors may contain sensitive data.
	// Only closed, fixed-message error types above are rendered.
	fmt.Fprintln(os.Stderr, "metis catalog inspect: CATALOG_FAILED: authorization, inspection or output failed")
	return 1
}
