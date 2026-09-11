import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

type Context = {
	concept: { id: string; path: string; type: string };
	concepts: Array<{ id: string; type: string; resource: string }>;
	raw: unknown;
	semantic: unknown;
	knowledge_root: string;
	writes_root: string;
};

const contextPath = process.env.S2SBENCH_OKFGEN_CONTEXT;
if (!contextPath) throw new Error("S2SBENCH_OKFGEN_CONTEXT is required");
const context: Context = JSON.parse(await readFile(contextPath, "utf8"));

function conceptID(params: unknown): string {
	const value = params as { concept_id?: unknown };
	if (typeof value?.concept_id !== "string" || value.concept_id !== context.concept.id) throw new Error(`concept_id must be ${context.concept.id}`);
	return value.concept_id;
}

function text(value: unknown) {
	return { content: [{ type: "text" as const, text: JSON.stringify(value, null, 2) }], details: value as Record<string, unknown> };
}

export default function (pi: ExtensionAPI) {
	pi.registerTool({ name: "list_concepts", label: "list_concepts", description: "List every source concept in the active OKF bundle.", parameters: { type: "object", properties: {} } as any, execute: async () => text(context.concepts) });
	pi.registerTool({ name: "read_existing_doc", label: "read_existing_doc", description: "Read the deterministic catalog-derived OKF document for this concept.", parameters: { type: "object", properties: { concept_id: { type: "string" } }, required: ["concept_id"] } as any, execute: async (_id, params) => { conceptID(params); return text({ path: context.concept.path, markdown: await readFile(join(context.knowledge_root, context.concept.path), "utf8") }); } });
	pi.registerTool({ name: "read_concept_raw", label: "read_concept_raw", description: "Read raw structured source metadata for this concept.", parameters: { type: "object", properties: { concept_id: { type: "string" } }, required: ["concept_id"] } as any, execute: async (_id, params) => { conceptID(params); return text(context.raw); } });
	pi.registerTool({ name: "read_semantic_context", label: "read_semantic_context", description: "Read the authoritative Ossie dataset, relationship, and metric definitions applicable to this concept.", parameters: { type: "object", properties: { concept_id: { type: "string" } }, required: ["concept_id"] } as any, execute: async (_id, params) => { conceptID(params); return text(context.semantic); } });
	pi.registerTool({ name: "sample_rows", label: "sample_rows", description: "Return the frozen-source sampling capability; this provider does not expose rows.", parameters: { type: "object", properties: { concept_id: { type: "string" }, n: { type: "integer" } }, required: ["concept_id"] } as any, execute: async (_id, params) => { conceptID(params); return text({ rows: [], note: "Sampling is unavailable for this frozen source catalog." }); } });
	pi.registerTool({ name: "write_concept_doc", label: "write_concept_doc", description: "Persist exactly one OKF document for this concept after its read tools.", parameters: { type: "object", properties: { concept_id: { type: "string" }, frontmatter: { type: "object" }, body: { type: "string" } }, required: ["concept_id", "frontmatter", "body"], additionalProperties: false } as any, execute: async (_id, params) => { conceptID(params); const write = params as { concept_id: string; frontmatter: Record<string, unknown>; body: string }; if (typeof write.body !== "string" || !write.frontmatter || typeof write.frontmatter !== "object") throw new Error("frontmatter object and body string are required"); await mkdir(context.writes_root, { recursive: true, mode: 0o700 }); const target = join(context.writes_root, context.concept.path.replace(/\.md$/, ".json")); await mkdir(dirname(target), { recursive: true, mode: 0o700 }); await writeFile(target, JSON.stringify(write), { encoding: "utf8", mode: 0o600 }); return text({ path: context.concept.path, bytes: Buffer.byteLength(JSON.stringify(write)) }); } });
}
