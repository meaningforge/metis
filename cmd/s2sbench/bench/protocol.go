package s2sbench

import "fmt"

const sharedEnvironmentBoundaries = `The benchmark harness, not you, executes and validates the answer.
Do not locate, open, or query the benchmark database, and do not inspect its data.
Do not inspect installed packages, binaries, source trees, or system directories.
Do not read, list, find, grep, or otherwise search filesystem paths outside the current workspace.
Once the semantic interface provides enough information to answer, return the required answer immediately instead of investigating the runtime environment.`

// RawAssetsSystemPrompt is the frozen protocol for path A. The agent keeps its
// normal harness; the semantic interface for this arm is the canonical Ossie
// model plus physical DuckDB schema in the canonical semantic project loaded
// by the Metis arm.
const RawAssetsSystemPrompt = `You are the SQL author for the raw-assets arm of a controlled benchmark.
Answer only the business question.
Use your normal agent tools, including shell, file operations, planning, and subagents, as needed.
Do not inspect Git metadata, repository history, branches, or unrelated project files.
` + sharedEnvironmentBoundaries + `
The semantic interface for this arm is the canonical semantic project and physical schema files in the current directory; obtain semantic definitions only from that interface.
The workspace contains multiple semantic models; identify the relevant model from the business question and available physical schema without assuming a preselected model.
Metis MCP is not available in this arm.
Return exactly one executable DuckDB SQL statement and no prose, markdown, or code fences.
Do not use or infer the benchmark's expected result.`

// MetisSystemPrompt is the frozen protocol for path B. The agent keeps the same
// normal harness; the semantic interface for this arm is the benchmark-owned
// Metis MCP server. S2SBench deterministically materializes the returned
// sql_render_result parameters for DuckDB execution.
const MetisSystemPrompt = `You are the semantic-query operator for the Metis arm of a controlled benchmark.
Answer only the business question.
Use your normal agent tools, including shell, file operations, planning, and subagents, as needed.
Do not inspect Git metadata, repository history, branches, or unrelated project files.
` + sharedEnvironmentBoundaries + `
The semantic interface for this arm is the benchmark-provided Metis MCP server; obtain semantic definitions only from that interface.
The MCP project contains multiple semantic models. Use the available semantic interface to answer the question.
Return exactly the JSON object from the compile result's sql_render_result field, with its dialect, sql, and parameters fields unchanged.
Return no prose, markdown, or code fences.
Do not hand-write replacement SQL, do not edit the compiled SQL, and do not use or infer the benchmark's expected result.`

const rawAgentTemplate = `%s

Business question:
%s

The current directory is the exact canonical semantic project loaded by the benchmark Metis server. It contains metis.yaml, the complete project under models/, and the current physical schema in schema.sql.`

const metisConfiguredAgentTemplate = `%s

Business question:
%s

The current directory is a clean workspace with no semantic source files. Metis MCP is the only semantic interface, and the application has already bound the active project context.`

const metisColdStartAgentTemplate = `%s

Business question:
%s

The current directory is a clean workspace with no semantic source files. Metis MCP is the only semantic interface, and the application has not bound an active project context.`

const repairTemplate = `Your previous answer failed with this execution or validation error:
%s

Repair only the answer. Do not use or infer any oracle/expected-result information.`

const metisRepairTemplate = `Your previous answer failed with this execution or validation error:
%s

Repair only the answer. If compile_sql already succeeded and the semantic request was correct, reuse that result without calling compile_sql again. Return exactly one complete sql_render_result JSON object and stop immediately after its closing brace. Do not use or infer any oracle/expected-result information.`

const rawContinuationTemplate = `Business question:
%s

This is the current independent benchmark question. The complete semantic project under models/ is unchanged; schema.sql has been refreshed for this question. Return exactly one executable DuckDB SQL statement and no prose, markdown, or code fences.`

const metisContinuationTemplate = `Business question:
%s

This is the current independent benchmark question in the same project. Use the same Metis MCP semantic interface. Return exactly the compile result's sql_render_result JSON object and no prose, markdown, or code fences.`

// RawAgentPrompt assembles the frozen path-A external-agent prompt. The actual
// canonical assets are files in the isolated workspace rather than being copied
// into the prompt, so normal coding agents exercise their real harness.
func RawAgentPrompt(question, failure string) string {
	return RawAgentTurnPrompt(question, failure, true)
}

func RawAgentTurnPrompt(question, failure string, first bool) string {
	prompt := fmt.Sprintf(rawContinuationTemplate, question)
	if first {
		prompt = fmt.Sprintf(rawAgentTemplate, RawAssetsSystemPrompt, question)
	}
	if failure != "" {
		prompt += "\n\n" + fmt.Sprintf(repairTemplate, failure)
	}
	return prompt
}

// MetisAgentPrompt assembles the frozen path-B external-agent prompt. A non-empty
// project selects cold-start wording but is deliberately not copied into the
// prompt; configured runs bind it through MCP transport context instead. The
// harness supplies no model, so semantic selection remains part of the task.
func MetisAgentPrompt(question, project, failure string) string {
	return MetisAgentTurnPrompt(question, project, failure, true)
}

func MetisAgentTurnPrompt(question, project, failure string, first bool) string {
	if !first {
		prompt := fmt.Sprintf(metisContinuationTemplate, question)
		if failure != "" {
			prompt += "\n\n" + fmt.Sprintf(metisRepairTemplate, failure)
		}
		return prompt
	}
	prompt := ""
	if project == "" {
		prompt = fmt.Sprintf(metisConfiguredAgentTemplate, MetisSystemPrompt, question)
	} else {
		prompt = fmt.Sprintf(metisColdStartAgentTemplate, MetisSystemPrompt, question)
	}
	if failure != "" {
		prompt += "\n\n" + fmt.Sprintf(metisRepairTemplate, failure)
	}
	return prompt
}

// The legacy client-level prompt helpers remain for deterministic harness tests
// and compatibility with the provider-neutral seams introduced in #460. Live
// S2SBench collection uses RawAgentPrompt/MetisAgentPrompt instead.
func RawAssetsUserPrompt(task RawAssetsTask, failure string) string {
	return RawAgentPrompt(task.Question, failure)
}

func MetisUserPrompt(task MetisTask, failure string) string {
	return MetisAgentPrompt(task.Question, task.Project, failure)
}
