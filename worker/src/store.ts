import { columnNames, createTableSQL, validateCreateTable } from "./rule";
import type { Rule, StoredRule } from "./types";

export interface RawEvent {
  captured_at: string;
  url: string;
  method: string;
  status: number;
  content_type: string;
  body: string;
  truncated: boolean;
  error: string;
}

function id(): string {
  return crypto.randomUUID().replace(/-/g, "");
}

function ident(name: string): string {
  if (!/^[a-z_][a-z0-9_]{0,62}$/.test(name)) throw new Error("invalid identifier");
  return `"${name}"`;
}

export async function listRules(db: D1Database): Promise<StoredRule[]> {
  const { results } = await db.prepare("SELECT id, pattern, rule, version, created_at, updated_at FROM parse_rules ORDER BY updated_at DESC").all<{
    id: string;
    pattern: string;
    rule: string;
    version: number;
    created_at: string;
    updated_at: string;
  }>();
  return (results ?? []).map((row) => ({
    id: row.id,
    pattern: row.pattern,
    rule: JSON.parse(row.rule) as Rule,
    version: row.version,
    created_at: row.created_at,
    updated_at: row.updated_at,
  }));
}

export async function upsertRule(db: D1Database, pattern: string, rule: Rule): Promise<StoredRule> {
  const now = new Date().toISOString();
  const existing = await db.prepare("SELECT id, version, created_at FROM parse_rules WHERE pattern = ?").bind(pattern).first<{
    id: string;
    version: number;
    created_at: string;
  }>();
  if (!existing) {
    const saved: StoredRule = { id: id(), pattern, rule, version: 1, created_at: now, updated_at: now };
    await db.prepare("INSERT INTO parse_rules (id, pattern, rule, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)").bind(
      saved.id, saved.pattern, JSON.stringify(rule), saved.version, saved.created_at, saved.updated_at,
    ).run();
    return saved;
  }
  const saved: StoredRule = {
    id: existing.id,
    pattern,
    rule,
    version: existing.version + 1,
    created_at: existing.created_at,
    updated_at: now,
  };
  await db.prepare("UPDATE parse_rules SET rule = ?, version = ?, updated_at = ? WHERE id = ?").bind(
    JSON.stringify(rule), saved.version, saved.updated_at, saved.id,
  ).run();
  return saved;
}

export async function insertRaw(db: D1Database, event: RawEvent): Promise<void> {
  await db.prepare(`INSERT INTO raw_events (captured_at, url, method, status, content_type, body, truncated, error)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`).bind(
    event.captured_at, event.url, event.method, event.status, event.content_type, event.body, event.truncated ? 1 : 0, event.error,
  ).run();
}

export async function insertRows(db: D1Database, table: string, rule: Rule, rows: Record<string, unknown>[]): Promise<void> {
  if (rows.length === 0) return;
  const columns = columnNames(rule);
  const sql = `INSERT INTO ${ident(table)} (${columns.map(ident).join(", ")}) VALUES (${columns.map(() => "?").join(", ")})`;
  await db.batch(rows.map((row) => db.prepare(sql).bind(...columns.map((column) => cell(row[column])))));
}

function cell(value: unknown): string | number | null {
  if (value == null) return null;
  if (typeof value === "number") return value;
  if (typeof value === "boolean") return value ? 1 : 0;
  return String(value);
}

export async function tableExists(db: D1Database, table: string): Promise<boolean> {
  ident(table);
  const row = await db.prepare("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?").bind(table).first();
  return Boolean(row);
}

async function tableColumns(db: D1Database, table: string): Promise<Set<string>> {
  const { results } = await db.prepare(`PRAGMA table_info(${ident(table)})`).all<{ name: string }>();
  return new Set((results ?? []).map((row) => row.name));
}

export async function execCreate(db: D1Database, statement: string, rule: Rule): Promise<void> {
  validateCreateTable(statement, rule);
  await db.prepare(statement).run();
}

export async function ensureTable(db: D1Database, rule: Rule): Promise<void> {
  if (await tableExists(db, rule.table)) {
    const have = await tableColumns(db, rule.table);
    const missing = columnNames(rule).filter((column) => !have.has(column));
    if (missing.length > 0) throw new Error(`table ${rule.table} is missing columns: ${missing.join(", ")}`);
    return;
  }
  await execCreate(db, createTableSQL(rule), rule);
  const have = await tableColumns(db, rule.table);
  const missing = columnNames(rule).filter((column) => !have.has(column));
  if (missing.length > 0) throw new Error(`table ${rule.table} is missing columns: ${missing.join(", ")}`);
}

export async function tryStartJob(db: D1Database, pattern: string): Promise<string | null> {
  const staleBefore = new Date(Date.now() - 2 * 60 * 1000).toISOString();
  await db.prepare(`UPDATE rule_jobs SET status = 'failed', error = 'stale', finished_at = ? WHERE pattern = ? AND status = 'running' AND created_at < ?`).bind(
    new Date().toISOString(), pattern, staleBefore,
  ).run();
  const jobID = id();
  try {
    await db.prepare("INSERT INTO rule_jobs (id, pattern, status, created_at) VALUES (?, ?, 'running', ?)").bind(
      jobID, pattern, new Date().toISOString(),
    ).run();
    return jobID;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message.toLowerCase().includes("unique")) return null;
    throw error;
  }
}

export async function finishJob(db: D1Database, jobID: string, status: string, error: string): Promise<void> {
  await db.prepare("UPDATE rule_jobs SET status = ?, error = ?, finished_at = ? WHERE id = ?").bind(
    status, error.slice(0, 500), new Date().toISOString(), jobID,
  ).run();
}
