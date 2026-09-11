// Command engines resolves registered real-engine dialect selectors for automation.
// The executable evidence registry remains the source of truth.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/meaningforge/metis/tests/conformance/evidence"
)

func main() {
	selector := flag.String("select", "all", "all or comma-separated registered real-engine dialects")
	flag.Parse()

	selected, err := selectTargets(evidence.RealEngineTargets(), *selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engines: %v\n", err)
		os.Exit(1)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		fmt.Fprintf(os.Stderr, "engines: encode targets: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}

func selectTargets(targets []evidence.Target, selector string) ([]string, error) {
	registered := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		dialect := strings.TrimSpace(strings.ToLower(target.Dialect))
		if dialect == "" {
			return nil, fmt.Errorf("registered real-engine target has empty dialect")
		}
		if _, exists := registered[dialect]; exists {
			return nil, fmt.Errorf("duplicate registered real-engine dialect %q", dialect)
		}
		registered[dialect] = struct{}{}
	}
	if len(registered) == 0 {
		return nil, fmt.Errorf("no real-engine targets are registered")
	}

	selector = strings.TrimSpace(strings.ToLower(selector))
	if selector == "" {
		return nil, fmt.Errorf("engine selector is required")
	}
	if selector == "all" {
		return sortedKeys(registered), nil
	}

	requested := make(map[string]struct{})
	for _, raw := range strings.Split(selector, ",") {
		dialect := strings.TrimSpace(raw)
		if dialect == "" {
			return nil, fmt.Errorf("engine selector %q contains an empty dialect", selector)
		}
		if dialect == "all" {
			return nil, fmt.Errorf("all cannot be combined with explicit engine dialects")
		}
		if _, ok := registered[dialect]; !ok {
			return nil, fmt.Errorf("engine %q is not registered; available: %s", dialect, strings.Join(sortedKeys(registered), ", "))
		}
		requested[dialect] = struct{}{}
	}
	return sortedKeys(requested), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
