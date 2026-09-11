package main

import (
	"fmt"
	"os"

	benchartifact "github.com/meaningforge/metis/cmd/s2sbench/bench/artifact"
	"github.com/meaningforge/metis/cmd/s2sbench/command"
)

func main() {
	if err := command.NewRoot(os.Stdout, os.Stderr).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, benchartifact.RedactString(err.Error()))
		os.Exit(1)
	}
}
