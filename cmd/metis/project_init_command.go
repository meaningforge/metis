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

	"github.com/meaningforge/metis/app/tooling/authoring"
)

func initProject(args []string) int {
	fs := flag.NewFlagSet("metis semantic init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	catalog := fs.String("catalog", "", "versioned catalog evidence (YAML or JSON)")
	mapping := fs.String("mapping", "", "explicit authoring map (YAML or JSON)")
	output := fs.String("output", "", "new candidate directory; existing paths are refused")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(*catalog) == "" || strings.TrimSpace(*mapping) == "" || strings.TrimSpace(*output) == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "metis semantic init: --catalog, --mapping, and --output are required; no positional arguments")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	report, err := authoring.InitProject(ctx, *catalog, *mapping, *output)
	if err != nil {
		var finding *authoring.Finding
		if errors.As(err, &finding) {
			fmt.Fprintln(os.Stderr, "metis semantic init:", finding)
			if finding.InvalidInput {
				return 2
			}
			return 1
		}
		fmt.Fprintln(os.Stderr, "metis semantic init: generation or I/O failed")
		return 2
	}
	fmt.Fprintf(os.Stdout, "candidate created: %s (project=%s model=%s); review authoring-report.json before adoption\n", *output, report.Project, report.Model)
	return 0
}
