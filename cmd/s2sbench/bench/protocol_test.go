package s2sbench

import (
	"strings"
	"testing"
)

func TestArmPromptsShareEnvironmentBoundaries(t *testing.T) {
	for name, prompt := range map[string]string{
		"raw_assets": RawAssetsSystemPrompt,
		"metis":      MetisSystemPrompt,
	} {
		t.Run(name, func(t *testing.T) {
			if count := strings.Count(prompt, sharedEnvironmentBoundaries); count != 1 {
				t.Fatalf("shared environment boundaries occur %d times, want 1 in prompt:\n%s", count, prompt)
			}
		})
	}

	for _, required := range []string{
		"harness, not you, executes and validates",
		"Do not locate, open, or query the benchmark database",
		"Do not inspect installed packages, binaries, source trees, or system directories",
		"outside the current workspace",
		"return the required answer immediately",
	} {
		if !strings.Contains(sharedEnvironmentBoundaries, required) {
			t.Errorf("shared environment boundaries omit %q", required)
		}
	}
}

func TestMetisRepairPromptReusesSuccessfulCompileAndStopsAfterJSON(t *testing.T) {
	prompt := MetisAgentTurnPrompt("question", "", "trailing content", false)
	for _, want := range []string{
		"reuse that result without calling compile_sql again",
		"exactly one complete render_result JSON object",
		"stop immediately after its closing brace",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt omits %q:\n%s", want, prompt)
		}
	}
}
