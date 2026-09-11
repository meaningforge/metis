// Command fingerprint emits a deterministic SHA-256 per scenario for every
// registered compiler target, for one of the archived fingerprint contracts.
//
// Byte identity is a hard gate on rendering changes
// and on the ansi-to-duckdb identity migration: a refactor that claims to move
// code without changing output has to prove it, and any hash that does move is a
// latent divergence between the three renderer forks rather than "part of the
// refactor".
//
// Both contracts are archived under
// tests/conformance/baseline/testdata so the universal-SemanticPlan migration
// starts from recorded evidence rather than a hash regenerated after the fact.
// The archives are gated by tests/conformance/baseline; this command is how they
// are read by hand and how an accepted movement is re-recorded.
//
//	go run ./tests/conformance/cmd/fingerprint > before.txt
//	# ...refactor...
//	go run ./tests/conformance/cmd/fingerprint > after.txt
//	diff before.txt after.txt
//
//	go run ./tests/conformance/cmd/fingerprint -kind=semantic-plan
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/meaningforge/metis/tests/conformance/baseline"
)

func main() {
	name := flag.String("kind", string(baseline.KindCompiledSQL), "baseline contract to emit: compiled-sql, compiled-parameters, or semantic-plan")
	flag.Parse()

	kind, err := baseline.ParseKind(*name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fingerprint: %v\n", err)
		os.Exit(2)
	}
	lines, err := baseline.Fingerprints(kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fingerprint: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(strings.Join(lines, "\n") + "\n")
}
