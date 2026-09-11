package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("S2SBench dependency and runtime-path boundaries: PASS")
}

func check() error {
	module, err := commandOutput("go", "list", "-m")
	if err != nil {
		return err
	}
	module = strings.TrimSpace(module)
	if err := checkProductionImports(module); err != nil {
		return err
	}
	for _, args := range [][]string{{"list", "-deps", "./cmd/s2sbench/..."}, {"list", "-deps", "-test", "./cmd/s2sbench/..."}} {
		output, err := commandOutput("go", args...)
		if err != nil {
			return err
		}
		for _, dependency := range strings.Fields(output) {
			if isForbiddenProductionImport(module, dependency) {
				return fmt.Errorf("S2SBench command dependency reaches test-owned package %q", dependency)
			}
		}
	}
	if err := filepath.WalkDir("cmd/s2sbench", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if containsForbiddenRuntimePath(string(body)) {
			return fmt.Errorf("repository-relative test runtime path found in %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	return checkTestImports(module)
}

func checkProductionImports(module string) error {
	output, err := commandOutput("go", "list", "-json", "./...")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(output))
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			return fmt.Errorf("decode go list output: %w", err)
		}
		if pkg.ImportPath == module+"/tests" || strings.HasPrefix(pkg.ImportPath, module+"/tests/") {
			continue
		}
		for _, dependency := range pkg.Imports {
			if isForbiddenProductionImport(module, dependency) {
				return fmt.Errorf("production package %q imports test-owned package %q", pkg.ImportPath, dependency)
			}
		}
	}
	return nil
}

func checkTestImports(module string) error {
	forbidden := module + "/cmd/s2sbench/"
	return filepath.WalkDir("tests", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if isForbiddenTestImport(forbidden, value) {
				return fmt.Errorf("test package %s imports S2SBench implementation package %q", path, value)
			}
		}
		return nil
	})
}

func isForbiddenProductionImport(module, imported string) bool {
	return imported == module+"/tests" || strings.HasPrefix(imported, module+"/tests/")
}

func isForbiddenTestImport(prefix, imported string) bool {
	if !strings.HasPrefix(imported, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(imported, prefix)
	return remainder == "command" || strings.HasPrefix(remainder, "command/") || remainder == "bench" || strings.HasPrefix(remainder, "bench/")
}

func containsForbiddenRuntimePath(source string) bool {
	scanner := bufio.NewScanner(strings.NewReader(source))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "tests/") || strings.Contains(line, `tests\\`) {
			return true
		}
	}
	return false
}

func commandOutput(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, exitErr.Stderr)
		}
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(output), nil
}
