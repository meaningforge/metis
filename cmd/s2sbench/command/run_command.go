package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	benchartifact "github.com/meaningforge/metis/cmd/s2sbench/bench/artifact"
)

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Type() string   { return "stringArray" }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func normalizeRunSelection(scenarioName string, repetitions int, repetitionsExplicit bool) (string, int, error) {
	scenarioName = strings.TrimSpace(scenarioName)
	if scenarioName == "" {
		return "", repetitions, nil
	}
	if repetitionsExplicit && repetitions != 1 {
		return "", 0, fmt.Errorf("--scenario smoke mode requires --repetitions 1 when repetitions are explicit")
	}
	return scenarioName, 1, nil
}

func validateRepairSelection(scenarioName string, repairAttempts int) error {
	if repairAttempts < 0 || repairAttempts >= s2sbench.FrozenBudget.Attempts {
		return fmt.Errorf("--repair-attempts must be 0 or %d", s2sbench.FrozenBudget.Attempts-1)
	}
	if strings.TrimSpace(scenarioName) == "" && repairAttempts != s2sbench.FrozenBudget.Attempts-1 {
		return fmt.Errorf("--repair-attempts 0 is only supported with --scenario smoke mode; formal and diagnostic runs retain the frozen repair budget")
	}
	return nil
}

func removeBenchmarkRuntimeRoot(root string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(root)
}

func validateAgentProvider(agentName, provider string) error {
	agentName = strings.TrimSpace(agentName)
	provider = strings.TrimSpace(provider)
	if agentName == "codex" && !strings.EqualFold(provider, "openai") {
		return fmt.Errorf("Codex benchmark harness uses the OpenAI provider, got --provider %q", provider)
	}
	return nil
}

func resolveExecutable(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return "", fmt.Errorf("executable name or path is required")
	}
	if strings.ContainsRune(value, os.PathSeparator) {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", fmt.Errorf("resolve executable path %q: %w", value, err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return "", fmt.Errorf("executable %q is not usable: %w", absolute, err)
		}
		if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return "", fmt.Errorf("executable %q is not an executable file", absolute)
		}
		return absolute, nil
	}
	resolved, err := exec.LookPath(value)
	if err != nil {
		return "", fmt.Errorf("executable %q not found in PATH: %w", value, err)
	}
	return resolved, nil
}

func buildAgentDriver(agentName, model, codexBin, claudeBin, genericBin, genericName, genericVersion string, genericArgs []string) (s2sbench.AgentDriver, error) {
	switch strings.TrimSpace(agentName) {
	case "codex":
		return newCodexDriver(codexBin, model)
	case "claude-code", "claude":
		return newClaudeCodeDriver(claudeBin, model)
	case "generic":
		identity := s2sbench.AgentIdentity{Name: strings.TrimSpace(genericName), Version: strings.TrimSpace(genericVersion), Model: strings.TrimSpace(model)}
		return newGenericDriver(genericBin, genericArgs, identity)
	default:
		return nil, fmt.Errorf("unsupported agent %q; want codex, claude-code, or generic", agentName)
	}
}

func persistRunBundle(output string, manifest s2sbench.RunManifest, collection s2sbench.Collection, report s2sbench.V0Report) error {
	journal, err := newRunJournal(output, manifest)
	if err != nil {
		return err
	}
	defer journal.Close()
	return journal.Commit(collection, report)
}

func writeJSONFile(path string, value any) error {
	return writeFile(path, func(w io.Writer) error {
		return writeJSON(w, value)
	})
}

func writeJSON(w io.Writer, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append([]byte(benchartifact.RedactString(string(body))), '\n')
	_, err = w.Write(body)
	return err
}

func writeFile(path string, write func(io.Writer) error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %q: %w", path, err)
	}
	ok = true
	return nil
}

func publishDirectory(partial, output string) error {
	if err := os.Rename(partial, output); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(output))
	if err != nil {
		return fmt.Errorf("open result parent for sync: %w", err)
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		return fmt.Errorf("sync published result parent: %w", err)
	}
	if err := parent.Close(); err != nil {
		return fmt.Errorf("close published result parent: %w", err)
	}
	return nil
}
