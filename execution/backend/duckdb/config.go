//go:build duckdb

package duckdb

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
)

func validateConfig(config map[string]string) error {
	for key := range config {
		if key != "path" {
			return fmt.Errorf("unsupported DuckDB config field %q", key)
		}
	}
	path, ok := config["path"]
	if !ok || strings.TrimSpace(path) == "" || strings.TrimSpace(path) != path {
		return fmt.Errorf("DuckDB config %q is required", "path")
	}
	if _, _, err := datasource.ParseSecretRef(path); err != nil {
		return fmt.Errorf("DuckDB config %q is invalid: %w", "path", err)
	}
	if path == ":memory:" {
		return fmt.Errorf("DuckDB config %q must reference a file-backed database", "path")
	}
	// duckdb-go parses connection options as a URL query. Reject query and
	// fragment delimiters so a deployment path cannot inject, replace, or hide
	// the Backend-owned read-only access mode.
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("DuckDB config %q must not contain a query or fragment component", "path")
	}
	return nil
}

func databasePath(config map[string]string, secrets driver.Secrets) (string, error) {
	configured, exists, err := driver.ConfigValue(config, secrets, "path")
	if err != nil {
		return "", err
	}
	resolved := map[string]string{}
	if exists {
		resolved["path"] = configured
	}
	if err := validateConfig(resolved); err != nil {
		return "", err
	}
	return configured, nil
}
