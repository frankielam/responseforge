import { HttpError, clipIngest, ingest, llmEnabled, putRule, regenerate, scheduleAfterIngest, scheduleRegenerate } from "./ingest";
import { listRules } from "./store";
import { PROTOCOL, type Env, type IngestRequest, type Rule } from "./types";

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const url = new URL(request.url);
    try {
      if (request.method === "GET" && url.pathname === "/healthz") {
        return json(200, { status: "ok", storage: "d1", llm: llmEnabled(env) });
      }
      if (!authorized(request, env)) return json(401, { error: "unauthorized" });
      if (request.method === "POST" && url.pathname === "/api/v1/ingest") {
        const payload = await readJSON(request);
        assertProtocol(payload.protocol, request.headers.get("accept"));
        const captured = payload.captured_at ? String(payload.captured_at) : new Date().toISOString();
        if (Number.isNaN(Date.parse(captured))) return json(400, { error: "captured_at must be RFC3339" });
        const req: IngestRequest = clipIngest({
          url: String(payload.url ?? ""),
          method: String(payload.method ?? ""),
          status: Number(payload.status),
          content_type: String(payload.content_type ?? ""),
          body: String(payload.body ?? ""),
          captured_at: new Date(captured).toISOString(),
          truncated: Boolean(payload.truncated),
        });
        const rulesBefore = await listRules(env.DB);
        const result = await ingest(env, req);
        if (result.llm === "scheduled") {
          const job = scheduleAfterIngest(env, req, rulesBefore);
          if (job) ctx.waitUntil(job);
        }
        return json(202, result);
      }
      if (request.method === "GET" && url.pathname === "/api/v1/rules") {
        return json(200, { rules: await listRules(env.DB) });
      }
      if (request.method === "PUT" && url.pathname === "/api/v1/rules") {
        const payload = await readJSON(request);
        assertProtocol(payload.protocol, request.headers.get("accept"));
        const source = (payload.rule ?? payload) as Rule;
        const rule: Rule = {
          pattern: source.pattern,
          content_types: source.content_types,
          record_path: source.record_path,
          table: source.table,
          fields: source.fields,
        };
        const saved = await putRule(env, rule, String(payload.sample_body ?? ""), String(payload.content_type ?? ""));
        return json(200, { rule: saved });
      }
      if (request.method === "POST" && url.pathname === "/api/v1/rules/regenerate") {
        const payload = await readJSON(request);
        assertProtocol(payload.protocol, request.headers.get("accept"));
        const pattern = await regenerate(
          env,
          String(payload.pattern ?? ""),
          String(payload.url ?? ""),
          String(payload.body ?? ""),
          String(payload.content_type ?? ""),
        );
        const rules = await listRules(env.DB);
        const old = rules.find((rule) => rule.pattern === pattern);
        ctx.waitUntil(scheduleRegenerate(env, pattern, String(payload.url ?? ""), String(payload.body ?? ""), String(payload.content_type ?? ""), old));
        return json(202, { llm: "scheduled", pattern });
      }
      return json(404, { error: "not found" });
    } catch (error) {
      if (error instanceof HttpError) return json(error.status, { error: error.message });
      if (error instanceof SyntaxError) return json(400, { error: "invalid json" });
      const message = error instanceof Error ? error.message : "store failed";
      if (/invalid|required|replay|pattern|table|field|selector|jsonpath|regex|create_table|unsupported protocol/i.test(message)) {
        return json(400, { error: message });
      }
      console.error(message);
      return json(500, { error: "store failed" });
    }
  },
};

function authorized(request: Request, env: Env): boolean {
  if (!env.INGEST_TOKEN) return !llmEnabled(env);
  return request.headers.get("authorization") === `Bearer ${env.INGEST_TOKEN}`;
}

async function readJSON(request: Request): Promise<Record<string, unknown>> {
  const text = await request.text();
  if (new TextEncoder().encode(text).length > 4 * 1024 * 1024) {
    throw new HttpError(413, "body too large");
  }
  const value = JSON.parse(text) as unknown;
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new SyntaxError("invalid json");
  return value as Record<string, unknown>;
}

function assertProtocol(value: unknown, accept: string | null): void {
  if (value != null && value !== PROTOCOL) {
    throw new HttpError(400, "unsupported protocol version");
  }
  const header = (accept ?? "").toLowerCase();
  const idx = header.indexOf("protocol=");
  if (idx < 0) return;
  const rest = header.slice(idx + "protocol=".length);
  if (!rest.startsWith("1") || (rest.length > 1 && !" ;,\t".includes(rest[1]!))) {
    throw new HttpError(400, "unsupported protocol version");
  }
}

function json(status: number, body: unknown): Response {
  const payload = status === 400 && body && typeof body === "object" && (body as { error?: string }).error === "unsupported protocol version"
    ? { ...(body as Record<string, unknown>), protocol: PROTOCOL }
    : body;
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json" },
  });
}
