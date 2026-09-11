import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

type MCPConfig = {
	url: string;
	bearerToken?: string;
	headers?: Record<string, string>;
};

const rawConfig = process.env.PI_S2SBENCH_MCP_CONFIG;
if (!rawConfig) throw new Error("PI_S2SBENCH_MCP_CONFIG is required");
const config = JSON.parse(rawConfig) as MCPConfig;

let nextID = 1;
let initialized: Promise<void> | undefined;
let sessionID = "";
let serverInstructions = "";

async function decodeResponse(response: Response): Promise<any> {
	const body = await response.text();
	if (!body.trim()) return undefined;
	if ((response.headers.get("content-type") ?? "").includes("text/event-stream")) {
		const data = body
			.split("\n")
			.filter((line) => line.startsWith("data:"))
			.map((line) => line.slice(5).trim())
			.find((line) => line && line !== "[DONE]");
		if (!data) throw new Error("Metis MCP returned an empty event stream");
		return JSON.parse(data);
	}
	return JSON.parse(body);
}

async function send(method: string, params?: unknown, notification = false): Promise<any> {
	const headers: Record<string, string> = {
		Accept: "application/json, text/event-stream",
		"Content-Type": "application/json",
		...(config.headers ?? {}),
	};
	if (config.bearerToken) headers.Authorization = `Bearer ${config.bearerToken}`;
	if (sessionID) headers["Mcp-Session-Id"] = sessionID;
	const request: Record<string, unknown> = { jsonrpc: "2.0", method };
	if (!notification) request.id = nextID++;
	if (params !== undefined) request.params = params;
	const response = await fetch(config.url, { method: "POST", headers, body: JSON.stringify(request) });
	if (!response.ok) throw new Error(`Metis MCP ${method} failed: HTTP ${response.status}: ${await response.text()}`);
	const returnedSession = response.headers.get("Mcp-Session-Id");
	if (returnedSession) sessionID = returnedSession;
	if (notification) return undefined;
	const decoded = await decodeResponse(response);
	if (decoded?.error) throw new Error(`Metis MCP ${method} failed: ${JSON.stringify(decoded.error)}`);
	return decoded?.result;
}

async function ensureInitialized(): Promise<void> {
	if (!initialized) {
		initialized = (async () => {
			const result = await send("initialize", {
				protocolVersion: "2025-03-26",
				capabilities: {},
				clientInfo: { name: "s2sbench-pi", version: "1" },
			});
			if (typeof result?.instructions === "string") serverInstructions = result.instructions.trim();
			await send("notifications/initialized", {}, true);
		})();
	}
	await initialized;
}

async function callTool(name: string, args: Record<string, unknown>) {
	await ensureInitialized();
	const result = await send("tools/call", { name, arguments: args });
	if (result?.isError) throw new Error(JSON.stringify(result));
	const structured = result?.structuredContent;
	// Content is the server's Agent-facing text representation. compile_sql also
	// retains the complete typed result in structuredContent/details for clients
	// that consume structured tool results.
	const contentText = (result?.content ?? []).filter((item: any) => item?.type === "text").map((item: any) => item.text).join("\n");
	const text = contentText || (structured === undefined ? "" : JSON.stringify(structured));
	return { content: [{ type: "text" as const, text: text || JSON.stringify(result) }], details: { result: structured ?? result } };
}

type MCPTool = {
	name: string;
	description?: string;
	inputSchema?: Record<string, unknown>;
};

async function discoverTools(): Promise<MCPTool[]> {
	await ensureInitialized();
	const tools: MCPTool[] = [];
	let cursor: string | undefined;
	do {
		const result = await send("tools/list", cursor ? { cursor } : {});
		if (!Array.isArray(result?.tools)) throw new Error("Metis MCP tools/list returned no tool array");
		tools.push(...result.tools);
		cursor = result.nextCursor || undefined;
	} while (cursor);
	return tools;
}

export default async function (pi: ExtensionAPI) {
	const tools = await discoverTools();
	if (tools.length === 0) throw new Error("Metis MCP exposed no tools");
	if (serverInstructions) {
		pi.on("before_agent_start", async (event) => ({
			systemPrompt: `${event.systemPrompt}\n\n${serverInstructions}`,
		}));
	}
	const seen = new Set<string>();
	for (const tool of tools) {
		if (!tool?.name || seen.has(tool.name)) throw new Error(`Metis MCP exposed an invalid or duplicate tool name: ${tool?.name}`);
		seen.add(tool.name);
		pi.registerTool({
			name: tool.name,
			label: tool.name,
			description: tool.description || `Metis MCP tool ${tool.name}`,
			parameters: (tool.inputSchema || { type: "object", properties: {} }) as any,
			execute: async (_id, params) => callTool(tool.name, params),
		});
	}
}
