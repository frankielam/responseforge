import { buildPrompt, completeChat, validateLLMOutput } from "./llm";
import { normalizeURL, selectRule, textual } from "./pattern";
import { parseBody } from "./parser";
import { createTableSQL, validateRule } from "./rule";
import { ensureTable, finishJob, insertRaw, insertRows, listRules, tableExists, tryStartJob, upsertRule, execCreate } from "./store";
import { MAX_BODY_BYTES, type Env, type IngestRequest, type IngestResult, type Rule, type StoredRule } from "./types";

export class HttpError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
}

export function llmEnabled(env: Env): boolean {
  return Boolean(env.LLM_BASE_URL?.trim() && env.LLM_MODEL?.trim());
}

function truncateBody(body: string, already: boolean): { body: string; truncated: boolean } {
  const bytes = new TextEncoder().encode(body);
  if (bytes.length <= MAX_BODY_BYTES) return { body, truncated: already };
  let end = MAX_BODY_BYTES;
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end--;
  return { body: new TextDecoder().decode(bytes.slice(0, end)), truncated: true };
}

export function clipIngest(req: IngestRequest): IngestRequest {
  const clipped = truncateBody(req.body, req.truncated);
  return { ...req, body: clipped.body, truncated: clipped.truncated, method: req.method.trim().toUpperCase() };
}

export async function ingest(env: Env, req: IngestRequest): Promise<IngestResult> {
  req = clipIngest(req);
  validate(req);
  const rules = await listRules(env.DB);
  const index = selectRule(
    rules.map((rule) => ({ pattern: rule.pattern, contentTypes: rule.rule.content_types, updatedAt: rule.updated_at })),
    req.url,
    req.content_type,
  );
  if (index >= 0) {
    const matched = rules[index]!;
    try {
      const rows = parseBody(matched.rule, req.content_type, req.body);
      try {
        await writeParsed(env, matched, req, rows);
      } catch (error) {
        const reason = error instanceof Error ? error.message : "store failed";
        await writeRaw(env, req, reason);
        return { stored: true, mode: "raw", pattern: matched.pattern, table: "raw_events", rows: 1, llm: llmEnabled(env) ? "skipped" : "disabled" };
      }
      return { stored: true, mode: "parsed", pattern: matched.pattern, table: matched.rule.table, rows: rows.length, llm: "skipped" };
    } catch (error) {
      const reason = error instanceof Error ? error.message : "parse failed";
      await writeRaw(env, req, reason);
      return {
        stored: true,
        mode: "raw",
        pattern: matched.pattern,
        table: "raw_events",
        rows: 1,
        llm: llmEnabled(env) ? (req.body.trim() ? "scheduled" : "skipped") : "disabled",
      };
    }
  }
  await writeRaw(env, req, "");
  let scheduled = false;
  if (llmEnabled(env) && req.body.trim()) {
    try {
      normalizeURL(req.url);
      scheduled = true;
    } catch {
      scheduled = false;
    }
  }
  return { stored: true, mode: "raw", table: "raw_events", rows: 1, llm: scheduled ? "scheduled" : "disabled" };
}

export function scheduleAfterIngest(env: Env, req: IngestRequest, rules: StoredRule[]): Promise<void> | null {
  if (!llmEnabled(env) || !req.body.trim()) return null;
  const index = selectRule(
    rules.map((rule) => ({ pattern: rule.pattern, contentTypes: rule.rule.content_types, updatedAt: rule.updated_at })),
    req.url,
    req.content_type,
  );
  if (index >= 0) {
    const matched = rules[index]!;
    try {
      parseBody(matched.rule, req.content_type, req.body);
      return null;
    } catch (error) {
      const reason = error instanceof Error ? error.message : "parse failed";
      return generate(env, matched.pattern, req, matched, reason);
    }
  }
  try {
    const pattern = normalizeURL(req.url);
    return generate(env, pattern, req, undefined, "");
  } catch {
    return null;
  }
}

export async function putRule(env: Env, rule: Rule, sampleBody: string, contentType: string): Promise<StoredRule> {
  validateRule(rule);
  if (sampleBody.trim()) {
    parseBody(rule, contentType || "application/json", sampleBody);
  }
  await ensureTable(env.DB, rule);
  return upsertRule(env.DB, rule.pattern, rule);
}

export async function regenerate(env: Env, pattern: string, url: string, body: string, contentType: string): Promise<string> {
  if (!llmEnabled(env)) throw new HttpError(400, "llm disabled");
  const resolved = pattern.trim() || normalizeURL(url);
  if (!body.trim()) throw new HttpError(400, "sample body is required");
  if (!textual(contentType || "application/json")) throw new HttpError(415, "unsupported content type");
  return resolved;
}

export function scheduleRegenerate(env: Env, pattern: string, url: string, body: string, contentType: string, old?: StoredRule): Promise<void> {
  const req: IngestRequest = {
    url: url || pattern,
    method: "GET",
    status: 200,
    content_type: contentType || "application/json",
    body,
    captured_at: new Date().toISOString(),
    truncated: false,
  };
  return generate(env, pattern, req, old, "manual regenerate");
}

async function writeRaw(env: Env, req: IngestRequest, error: string): Promise<void> {
  await insertRaw(env.DB, {
    captured_at: req.captured_at,
    url: req.url,
    method: req.method,
    status: req.status,
    content_type: req.content_type,
    body: req.body,
    truncated: req.truncated,
    error,
  });
}

async function writeParsed(env: Env, saved: StoredRule, req: IngestRequest, rows: Record<string, unknown>[]): Promise<void> {
  await ensureTable(env.DB, saved.rule);
  for (const row of rows) {
    row._captured_at = req.captured_at;
    row._source_url = req.url;
    row._rule_id = saved.id;
    row._rule_version = saved.version;
  }
  await insertRows(env.DB, saved.rule.table, saved.rule, rows);
}

async function generate(env: Env, pattern: string, sample: IngestRequest, old: StoredRule | undefined, failure: string): Promise<void> {
  const jobID = await tryStartJob(env.DB, pattern);
  if (!jobID) return;
  let status = "succeeded";
  let error = "";
  try {
    await runModel(env, pattern, sample, old, failure);
  } catch (err) {
    status = "failed";
    error = err instanceof Error ? err.message : "llm failed";
    console.error("llm job failed", pattern, error);
  }
  await finishJob(env.DB, jobID, status, error);
}

async function runModel(env: Env, pattern: string, sample: IngestRequest, old: StoredRule | undefined, failure: string): Promise<void> {
  const timeoutSeconds = Number(env.LLM_TIMEOUT_SECONDS || "90");
  const timeout = Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 ? timeoutSeconds * 1000 : 30_000;
  const maxSampleValue = Number(env.LLM_MAX_SAMPLE_CHARS || "12000");
  const maxSample = Number.isFinite(maxSampleValue) && maxSampleValue > 0 ? Math.floor(maxSampleValue) : 12_000;
  const text = await completeChat(
    env.LLM_BASE_URL || "",
    env.LLM_API_KEY || "",
    env.LLM_MODEL || "",
    buildPrompt({
      url: sample.url,
      pattern,
      contentType: sample.content_type,
      body: sample.body,
      needCreate: true,
      oldRule: old?.rule,
      failure,
    maxSampleChars: maxSample,
    }),
    timeout,
  );
  const out = validateLLMOutput(text, sample.body, sample.content_type, false, pattern);
  if (old && out.rule.table !== old.rule.table) throw new Error(`repair must keep table ${old.rule.table}`);
  if (await tableExists(env.DB, out.rule.table)) {
    await ensureTable(env.DB, out.rule);
  } else {
    await execCreate(env.DB, createTableSQL(out.rule), out.rule);
    await ensureTable(env.DB, out.rule);
  }
  await upsertRule(env.DB, pattern, out.rule);
}

function validate(req: IngestRequest): void {
  let url: URL;
  try {
    url = new URL(req.url);
  } catch {
    throw new HttpError(400, "url must be an absolute http(s) url");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new HttpError(400, "url must be an absolute http(s) url");
  if (req.url.length > 4096) throw new HttpError(400, "url is too long");
  if (!req.method || req.method.length > 16 || !/^[A-Z]+$/.test(req.method)) throw new HttpError(400, "method is invalid");
  if (req.status < 100 || req.status > 599) throw new HttpError(400, "status is invalid");
  if (!textual(req.content_type)) throw new HttpError(415, "unsupported content type");
}
