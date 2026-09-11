#!/usr/bin/env node

import { createInterface } from "node:readline";
import { spawn } from "node:child_process";
import { chmod, copyFile, mkdir, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const protocol = "s2sbench-driver-v2";
const extension = join(dirname(fileURLToPath(import.meta.url)), "metis.ts");
const provider = process.env.S2SBENCH_PI_PROVIDER || "deepseek";
const model = process.env.S2SBENCH_PI_MODEL || "deepseek-v4-flash";
const piBinary = process.env.S2SBENCH_PI_BIN || "pi";
const timeoutMS = Number(process.env.S2SBENCH_PI_TIMEOUT_MS || 60000);
let turn = 0;
let sessionPath = "";

async function seedIsolatedPiAuth() {
	const source = process.env.S2SBENCH_PI_AUTH_FILE;
	if (!source) return;
	const home = process.env.HOME;
	if (!home) throw new Error("Pi authentication requires an isolated HOME");
	const destinationDir = join(home, ".pi", "agent");
	const destination = join(destinationDir, "auth.json");
	try {
		const sourceInfo = await stat(source);
		if (!sourceInfo.isFile()) throw new Error("source is not a regular file");
		await mkdir(destinationDir, { recursive: true, mode: 0o700 });
		await copyFile(source, destination);
		await chmod(destination, 0o600);
	} catch (error) {
		throw new Error(`prepare isolated Pi authentication: ${error?.message || error}`);
	}
}

function assistantText(message) {
	if (!message || message.role !== "assistant" || !Array.isArray(message.content)) return "";
	return message.content.filter((item) => item.type === "text").map((item) => item.text || "").join("");
}

function usageOf(message) {
	const usage = message?.usage || {};
	return {
		context: Number(usage.input || 0) + Number(usage.cacheRead || 0) + Number(usage.cacheWrite || 0),
		output: Number(usage.output || 0),
	};
}

function encodedBytes(value) {
	if (value === undefined) return 0;
	try { return Buffer.byteLength(JSON.stringify(value)); } catch { return 0; }
}

const metisTools = new Set([
	"list_projects", "list_models", "get_model", "list_metrics", "get_metric",
	"get_dimensions", "get_dimension", "get_dimension_values", "get_relationships", "compile_sql",
	"query_metrics", "attribute_metric", "compare_metrics",
]);

function isMetisTool(name) {
	return [...metisTools].some((tool) => name === tool || name.endsWith(`.${tool}`) || name.endsWith(`_${tool}`));
}

function redactArguments(value) {
	if (Array.isArray(value)) return value.map(redactArguments);
	if (!value || typeof value !== "object") return value;
	const result = {};
	for (const [key, item] of Object.entries(value)) {
		const normalized = key.toLowerCase().replaceAll("-", "_");
		result[key] = ["token", "secret", "password", "authorization", "api_key"].some((part) => normalized.includes(part))
			? "[REDACTED]"
			: redactArguments(item);
	}
	return result;
}

function boundedArguments(name, value) {
	if (!isMetisTool(name) || value === undefined) return undefined;
	try {
		const encoded = JSON.stringify(redactArguments(value));
		return Buffer.byteLength(encoded) <= 4096
			? encoded
			: JSON.stringify({ truncated: true, request_bytes: encodedBytes(value) });
	} catch {
		return undefined;
	}
}

function boundedSemanticResponse(name, value) {
	if (!(name === "compile_sql" || name.endsWith(".compile_sql") || name.endsWith("_compile_sql") || name === "query_metrics" || name.endsWith(".query_metrics") || name.endsWith("_query_metrics")) || value === undefined) return undefined;
	try {
		const encoded = typeof value === "string" ? value : JSON.stringify(value);
		return Buffer.byteLength(encoded) <= 16777216 ? encoded : undefined;
	} catch {
		return undefined;
	}
}

function completeToolTrace(toolTrace, toolStartedAt, event) {
	const name = String(event.toolName || "unknown");
	for (let i = toolTrace.length - 1; i >= 0; i--) {
		const entry = toolTrace[i];
		if (entry.name !== name || entry.status !== "incomplete") continue;
		const startedAt = toolStartedAt[i];
		if (Number.isFinite(startedAt)) entry.duration_ms = Math.max(0, Date.now() - startedAt);
		entry.status = event.isError ? "error" : "success";
		entry.response_bytes = encodedBytes(event.result ?? event.output);
		if (!event.isError) entry.response = boundedSemanticResponse(name, event.result ?? event.output);
		if (event.isError) {
			const detail = event.error ?? event.result ?? event.output ?? "tool execution failed";
			let summary;
			try { summary = typeof detail === "string" ? detail : JSON.stringify(detail); } catch { summary = String(detail); }
			entry.error = summary.trim().slice(0, 4096);
		}
		return;
	}
}

async function runPi(request) {
	if (!sessionPath) sessionPath = join(process.env.TMPDIR || "/tmp", `s2sbench-pi-${process.pid}.jsonl`);
	await seedIsolatedPiAuth();
	const metis = request.mcp;
	const args = [
		"--mode", "json",
		"--provider", provider,
		"--model", model,
		"--thinking", "low",
		"--session", sessionPath,
		"--approve",
	];
	// Both arms keep Pi's native default capabilities. The Metis arm differs only
	// by loading the benchmark-owned MCP forwarding extension.
	if (metis) args.push("--extension", extension);
	args.push(request.prompt);

	const environment = { ...process.env };
	if (metis) {
		environment.PI_S2SBENCH_MCP_CONFIG = JSON.stringify({
			url: metis.url,
			bearerToken: metis.bearer_token_env_var ? process.env[metis.bearer_token_env_var] : undefined,
			headers: metis.http_headers || {},
		});
	}

	return await new Promise((resolve, reject) => {
		const child = spawn(piBinary, args, { cwd: request.workspace, env: environment, stdio: ["ignore", "pipe", "pipe"] });
		let stdout = "";
		let stderr = "";
		let toolCalls = 0;
		let contextTokens = 0;
		let outputTokens = 0;
		let output = "";
		let killedForBudget = false;
		let killedForTimeout = false;
		let completedByQuery = false;
		const trace = [];
		const toolTrace = [];
		const toolStartedAt = [];
		const maxToolCalls = Number(request.budget.max_tool_calls || 0);
		const requestedTurnTimeoutMS = Number(request.turn_timeout_ms || 0);
		const turnTimeoutMS = requestedTurnTimeoutMS > 0 ? Math.min(timeoutMS, requestedTurnTimeoutMS) : timeoutMS;
		const timer = turnTimeoutMS > 0 ? setTimeout(() => {
			killedForTimeout = true;
			child.kill("SIGTERM");
		}, turnTimeoutMS) : undefined;

		child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
		child.stdout.on("data", (chunk) => {
			stdout += chunk.toString();
			while (true) {
				const newline = stdout.indexOf("\n");
				if (newline < 0) break;
				const line = stdout.slice(0, newline).trim();
				stdout = stdout.slice(newline + 1);
				if (!line) continue;
				let event;
				try { event = JSON.parse(line); } catch { continue; }
				if (event.type === "tool_execution_start") {
					// Always retain the raw diagnostic event. Normalized accounting stops at
					// the first over-budget call so buffered JSON events after SIGTERM cannot
					// turn a terminal 21/20 overrun into 22/20 or higher.
					trace.push({ type: event.type, tool: event.toolName });
					if (!killedForBudget) {
						toolCalls++;
						const traceIndex = toolTrace.length;
						toolTrace.push({
							name: String(event.toolName || "unknown"),
							duration_ms: 0,
							request_bytes: encodedBytes(event.args ?? event.input ?? event.parameters),
							arguments: boundedArguments(String(event.toolName || "unknown"), event.args ?? event.input ?? event.parameters),
							response_bytes: 0,
							status: "incomplete",
						});
						toolStartedAt[traceIndex] = Date.now();
						if (toolCalls > maxToolCalls) {
							killedForBudget = true;
							child.kill("SIGTERM");
						}
					}
				}
				if (event.type === "tool_execution_end") {
					trace.push({ type: event.type, tool: event.toolName, error: !!event.isError });
					completeToolTrace(toolTrace, toolStartedAt, event);
					const name = String(event.toolName || "");
					if (metis && !event.isError && (name === "query_metrics" || name.endsWith(".query_metrics") || name.endsWith("_query_metrics")) && boundedSemanticResponse(name, event.result ?? event.output)) {
						completedByQuery = true;
						output = '{"status":"queried"}';
						child.kill("SIGTERM");
					}
				}
				if (event.type === "message_end" && event.message?.role === "assistant") {
					const usage = usageOf(event.message);
					contextTokens += usage.context;
					outputTokens += usage.output;
					const text = assistantText(event.message).trim();
					if (text) output = text;
				}
			}
		});
		child.on("error", reject);
		child.on("close", (code, signal) => {
			if (timer) clearTimeout(timer);
			if (killedForBudget) output ||= `Pi exceeded tool-call budget ${maxToolCalls}`;
			if (!output && !killedForTimeout && !completedByQuery && (code !== 0 || signal)) return reject(new Error(`pi failed (${signal || code}): ${stderr.trim()}`));
			resolve({
				protocol_version: protocol,
				output,
				error: killedForTimeout ? `Pi exceeded turn timeout ${turnTimeoutMS}ms` : undefined,
				error_kind: killedForTimeout ? "timeout" : undefined,
				tool_calls: toolCalls,
				context_tokens: contextTokens,
				output_tokens: outputTokens,
				tool_trace: toolTrace,
				transcript: JSON.stringify({ turn: ++turn, tools: trace, stderr: stderr.trim().slice(0, 512) }),
			});
		});
	});
}

const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of lines) {
	if (!line.trim()) continue;
	try {
		const request = JSON.parse(line);
		if (request.protocol_version !== protocol) throw new Error(`unsupported protocol ${request.protocol_version}`);
		process.stdout.write(`${JSON.stringify(await runPi(request))}\n`);
	} catch (error) {
		process.stderr.write(`${error?.stack || error}\n`);
		process.exitCode = 1;
		break;
	}
}
