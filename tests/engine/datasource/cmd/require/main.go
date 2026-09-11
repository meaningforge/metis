package main

import (
	"flag"
	"fmt"
	"os"

	testdatasource "github.com/meaningforge/metis/tests/engine/datasource"
)

func main() {
	name := flag.String("datasource", "", "checked-in test DataSource name")
	flag.Parse()
	if *name == "" {
		fmt.Fprintln(os.Stderr, "-datasource is required")
		os.Exit(2)
	}
	if _, err := testdatasource.Resolve(*name, os.LookupEnv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
