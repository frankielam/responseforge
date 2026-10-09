import { SYSTEM_COLUMNS, type Field, type Rule } from "./types";

const TABLE_RE = /^[a-z][a-z0-9_]{0,62}$/;
const FIELD_RE = /^[a-z_][a-z0-9_]{0,62}$/;
const JSON_PATH_RE = /^\$(\.[A-Za-z_][A-Za-z0-9_]*|\[['"][^'"\]]+['"]\]|\[\d+\]|\[\*\])*$/;
const DANGEROUS_RE = /(<script|javascript\s*:|\beval\s*\(|\bfunction\s*\(|\bimport\b|\brequire\b|=>)/i;
const BANNED_SQL =
  /(^|[^A-Z0-9_])(DROP|ALTER|ATTACH|DETACH|PRAGMA|DELETE|INSERT|UPDATE|TRUNCATE|GRANT|REVOKE|VACUUM|COPY|CALL|EXECUTE|UNION|SELECT|INTO|FROM|WHERE|JOIN|FOREIGN|REFERENCES|TRIGGER|PROCEDURE|MERGE|REPLACE|LOAD|ANALYSE|ANALYZE)([^A-Z0-9_]|$)/i;

export function validateRule(rule: Rule): void {
  if (!rule || typeof rule !== "object") throw new Error("rule must be an object");
  if (typeof rule.pattern !== "string" || !rule.pattern.trim() || rule.pattern.length > 4096 || /[\r\n]/.test(rule.pattern) || rule.pattern.includes("**")) {
    throw new Error("pattern is required");
  }
  if (typeof rule.table !== "string" || !TABLE_RE.test(rule.table)) throw new Error("table name must match [a-z][a-z0-9_]{0,62}");
  if (rule.content_types !== undefined) {
    if (!Array.isArray(rule.content_types) || rule.content_types.some((value) => typeof value !== "string" || !value.trim() || /[\r\n]/.test(value))) {
      throw new Error("content_types must be an array of strings");
    }
  }
  if (rule.record_path !== undefined && typeof rule.record_path !== "string") {
    throw new Error("record_path must be a string");
  }
  if (!Array.isArray(rule.fields) || rule.fields.length === 0) throw new Error("at least one field is required");
  if (rule.fields.length > 256) throw new Error("too many fields");
  if (rule.content_types && rule.content_types.length > 64) throw new Error("too many content types");
  const seen = new Set<string>();
  for (const field of rule.fields) {
    if (!field || typeof field !== "object") throw new Error("field must be an object");
    if (typeof field.name !== "string" || !FIELD_RE.test(field.name)) throw new Error(`invalid field name ${String(field.name)}`);
    if (seen.has(field.name)) throw new Error(`duplicate field ${field.name}`);
    seen.add(field.name);
    if (typeof field.from !== "string") throw new Error(`field ${field.name} has an invalid from`);
    if (!["string", "number", "boolean", "timestamp"].includes(field.type)) {
      throw new Error(`field ${field.name} has unsupported type`);
    }
    if (field.required !== undefined && typeof field.required !== "boolean") {
      throw new Error(`field ${field.name} required must be boolean`);
    }
    if (field.attr !== undefined && typeof field.attr !== "string") {
      throw new Error(`field ${field.name} attr must be a string`);
    }
    validateFrom(field.from);
    if (field.attr && /[\r\n"'<>]/.test(field.attr)) throw new Error(`field ${field.name} has an invalid attr`);
  }
  if (rule.record_path) validateRecordPath(rule.record_path);
}

function validateFrom(from: string): void {
  if (!from?.trim() || from.length > 500 || /[\r\n]/.test(from)) {
    throw new Error("from must be a single line of at most 500 characters");
  }
  if (DANGEROUS_RE.test(from)) throw new Error("from must be a declarative extractor");
  const kind = classify(from);
  if (kind === "jsonpath" && !JSON_PATH_RE.test(from)) throw new Error("jsonpath is not allowed");
  if (kind === "regex") compileNamed(from);
  if (kind === "css" && /[{};]/.test(from)) throw new Error("css selector must not contain braces or semicolons");
}

function validateRecordPath(path: string): void {
  if (path.length > 500 || /[\r\n]/.test(path) || DANGEROUS_RE.test(path)) throw new Error("invalid record_path");
  if (path.startsWith("$") && !JSON_PATH_RE.test(path)) throw new Error("record_path jsonpath is not allowed");
  if (classify(path) === "regex") compileNamed(path);
}

export function classify(from: string): "jsonpath" | "regex" | "css" {
  if (from.startsWith("$")) return "jsonpath";
  if (from.includes("(?P<") || from.includes("(?<")) return "regex";
  return "css";
}

export function compileNamed(expr: string): RegExp {
  if (expr.length > 300) throw new Error("regex is too long");
  if (unsafeRegex(expr)) throw new Error("unsafe regex");
  const js = expr.replace(/\(\?P<([A-Za-z_][A-Za-z0-9_]*)>/g, "(?<$1>");
  let re: RegExp;
  try {
    re = new RegExp(js);
  } catch {
    throw new Error("invalid regex");
  }
  const names = [...js.matchAll(/\(\?<([A-Za-z_][A-Za-z0-9_]*)>/g)].map((m) => m[1]);
  if (names.length !== 1) throw new Error("regex must contain exactly one named group");
  return re;
}

function unsafeRegex(expr: string): boolean {
  let depth = 0;
  let inClass = false;
  let escaped = false;
  for (let i = 0; i < expr.length; i++) {
    const ch = expr[i]!;
    if (escaped) {
      if (ch >= "1" && ch <= "9") return true;
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      continue;
    }
    if (ch === "[") {
      inClass = true;
      continue;
    }
    if (ch === "]") {
      inClass = false;
      continue;
    }
    if (inClass) continue;
    if (ch === "(") {
      if (expr.startsWith("(?=", i) || expr.startsWith("(?!", i) || expr.startsWith("(?<=", i) || expr.startsWith("(?<!", i)) return true;
      depth++;
      continue;
    }
    if (ch === ")") {
      depth = Math.max(0, depth - 1);
      if (expr[i + 1] === "*" || expr[i + 1] === "+" || expr[i + 1] === "{") return true;
      continue;
    }
    if ((ch === "*" || ch === "+" || ch === "{") && depth > 1) return true;
  }
  return false;
}

export function columnNames(rule: Rule): string[] {
  return [...rule.fields.map((field) => field.name), ...SYSTEM_COLUMNS];
}

export function validateCreateTable(statement: string, rule: Rule): void {
  validateRule(rule);
  let sql = statement.trim();
  if (!sql) throw new Error("create_table is required");
  if (sql.includes("--") || sql.includes("/*") || sql.includes("*/")) throw new Error("sql comments are not allowed");
  if (sql.endsWith(";")) sql = sql.slice(0, -1).trim();
  if (sql.includes(";")) throw new Error("multiple statements are not allowed");
  if (BANNED_SQL.test(sql)) throw new Error("statement contains a forbidden keyword");
  const upper = sql.toUpperCase();
  for (const bad of ["BIGSERIAL", "JSONB", "SERIAL", "NEXTVAL"]) {
    if (upper.includes(bad)) throw new Error("statement uses a non-sqlite type");
  }
  const match = /^CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+("?)([a-z][a-z0-9_]{0,62})"?\s*\((.*)\)\s*$/is.exec(sql);
  if (!match || match[2] !== rule.table) throw new Error("only a single CREATE TABLE IF NOT EXISTS for rule.table is allowed");
  const have = new Set(columnNamesFromSQL(match[3] ?? ""));
  for (const want of columnNames(rule)) {
    if (!have.has(want)) throw new Error(`create_table is missing column ${want}`);
  }
  if (normalizeSQL(sql) !== normalizeSQL(createTableSQL(rule))) {
    throw new Error("create_table must match the server-generated schema");
  }
}

function normalizeSQL(value: string): string {
  return value.trim().toLowerCase().split(/\s+/).join(" ");
}

function columnNamesFromSQL(body: string): string[] {
  const names: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < body.length; i++) {
    const ch = body[i];
    if (ch === "(") depth++;
    else if (ch === ")" && depth > 0) depth--;
    else if (ch === "," && depth === 0) {
      const name = firstIdent(body.slice(start, i));
      if (name) names.push(name);
      start = i + 1;
    }
  }
  const last = firstIdent(body.slice(start));
  if (last) names.push(last);
  return names;
}

function firstIdent(part: string): string {
  const s = part.trim();
  if (!s) return "";
  if (s.startsWith('"') || s.startsWith("`")) {
    const end = s.indexOf(s[0]!, 1);
    return end > 0 ? s.slice(1, end).toLowerCase() : "";
  }
  const match = /^[A-Za-z_][A-Za-z0-9_]*/.exec(s);
  return match ? match[0].toLowerCase() : "";
}

export function createTableSQL(rule: Rule): string {
  validateRule(rule);
  const cols = rule.fields.map((field) => `${field.name} ${sqlType(field)}`);
  for (const col of SYSTEM_COLUMNS) cols.push(`${col} ${col === "_rule_version" ? "INTEGER" : "TEXT"}`);
  return `CREATE TABLE IF NOT EXISTS ${rule.table} (${cols.join(", ")})`;
}

function sqlType(field: Field): string {
  if (field.type === "number") return "REAL";
  if (field.type === "boolean") return "INTEGER";
  return "TEXT";
}
