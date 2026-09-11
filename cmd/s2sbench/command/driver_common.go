package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

const benchmarkMCPServerName = "metis"

const maxToolArgumentSummaryBytes = 4096

func boundedToolArguments(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	encoded, err := json.Marshal(redactToolArgumentValue(value))
	if err != nil {
		return ""
	}
	if len(encoded) <= maxToolArgumentSummaryBytes {
		return string(encoded)
	}
	fallback, _ := json.Marshal(map[string]any{"truncated": true, "request_bytes": len(raw)})
	return string(fallback)
}

func redactToolArgumentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "api_key") {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = redactToolArgumentValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = redactToolArgumentValue(typed[i])
		}
		return result
	default:
		return value
	}
}

type mcpEnvironmentBaseline struct {
	mu sync.Mutex
}

// Validate enforces the clean benchmark MCP environment. Raw sessions expose
// no MCP servers; Metis sessions expose exactly the benchmark-owned server.
func (b *mcpEnvironmentBaseline) Validate(active []string, added string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	active, err := normalizedNames(active)
	if err != nil {
		return nil, err
	}
	added = strings.TrimSpace(added)
	want := []string{}
	if added == "" {
		if containsString(active, benchmarkMCPServerName) {
			return nil, fmt.Errorf("isolated raw environment contains benchmark MCP server %q", benchmarkMCPServerName)
		}
	} else if added != benchmarkMCPServerName {
		return nil, fmt.Errorf("benchmark MCP server is %q, got %q", benchmarkMCPServerName, added)
	} else {
		want = []string{benchmarkMCPServerName}
	}
	if !equalStrings(active, want) {
		return nil, fmt.Errorf("agent MCP environment is %v, want isolated benchmark environment %v", active, want)
	}
	return append([]string(nil), active...), nil
}

func normalizedNames(names []string) ([]string, error) {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("MCP environment contains an empty server name")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("MCP environment contains duplicate server %q", name)
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateSessionRequest(initial, next s2sbench.AgentRequest) error {
	if err := validateAgentRequest(next); err != nil {
		return err
	}
	left, right := initial, next
	left.Prompt, right.Prompt = "", ""
	left.Budget, right.Budget = s2sbench.TaskBudget{}, s2sbench.TaskBudget{}
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("agent session configuration changed between turns")
	}
	return nil
}

var agentNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateAgentRequest(request s2sbench.AgentRequest) error {
	if strings.TrimSpace(request.Workspace) == "" {
		return fmt.Errorf("agent workspace is required")
	}
	info, err := os.Stat(request.Workspace)
	if err != nil {
		return fmt.Errorf("agent workspace: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("agent workspace %q is not a directory", request.Workspace)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return fmt.Errorf("agent prompt is required")
	}
	if request.Budget.ToolCalls < 0 || request.Budget.Attempts < 1 || request.Budget.QuestionTimeoutMS < 0 || request.Budget.Repetitions < 1 {
		return fmt.Errorf("invalid agent budget %+v", request.Budget)
	}
	for name, value := range request.Environment {
		if !agentEnvironmentNamePattern.MatchString(name) {
			return fmt.Errorf("invalid agent environment variable name %q", name)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("agent environment variable %q contains NUL", name)
		}
	}
	return nil
}

var agentEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func prepareIsolatedAgentHome(prefix string) (string, error) {
	home, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(filepath.Join(home, "tmp"), 0o700); err != nil {
		_ = os.RemoveAll(home)
		return "", err
	}
	return home, nil
}

func agentCommandEnvironment(request s2sbench.AgentRequest, home string) []string {
	values := map[string]string{"HOME": home, "TMPDIR": filepath.Join(home, "tmp")}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if isolatedAgentEnvironmentVariable(key) {
			values[key] = value
		}
	}
	// Proxy variables are conventionally accepted in either case, but callers
	// and HTTP stacks are inconsistent about which spelling they honor. Preserve
	// the source spelling and synthesize a missing uppercase spelling so an
	// isolated Agent does not silently lose the host's only network route.
	for lower, upper := range map[string]string{
		"http_proxy": "HTTP_PROXY", "https_proxy": "HTTPS_PROXY",
		"all_proxy": "ALL_PROXY", "no_proxy": "NO_PROXY",
	} {
		if values[upper] == "" && values[lower] != "" {
			values[upper] = values[lower]
		}
	}
	for key, value := range request.Environment {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func isolatedAgentEnvironmentVariable(name string) bool {
	switch name {
	case "PATH", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP", "LANG", "TERM",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
		"__CF_USER_TEXT_ENCODING", "S2SBENCH_PI_BIN", "S2SBENCH_PI_PROVIDER", "S2SBENCH_PI_MODEL", "S2SBENCH_PI_TIMEOUT_MS", "S2SBENCH_PI_AUTH_FILE":
		return true
	}
	for _, prefix := range []string{"LC_", "ANTHROPIC_", "OPENAI_", "DEEPSEEK_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func environmentWithOverrides(environment []string, overrides map[string]string) []string {
	values := make(map[string]string, len(environment)+len(overrides))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	for name, value := range overrides {
		values[name] = value
	}
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, name := range keys {
		result = append(result, name+"="+values[name])
	}
	return result
}

func commandVersion(ctx context.Context, binary string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", binary, err, strings.TrimSpace(string(output)))
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", fmt.Errorf("%s returned an empty version", binary)
	}
	return version, nil
}
