import { parse as parseHTMLDocument } from "node-html-parser";
import { kind } from "./pattern";
import { classify, compileNamed, validateRule } from "./rule";
import type { Field, Rule } from "./types";

type Row = Record<string, unknown>;

export function parseBody(rule: Rule, contentType: string, body: string): Row[] {
  validateRule(rule);
  const family = kind(contentType);
  if (family === "json") return parseJSON(rule, body);
  if (family === "html") return parseHTML(rule, body);
  return parseText(rule, body);
}

function parseJSON(rule: Rule, body: string): Row[] {
  let root: unknown;
  try {
    root = JSON.parse(body);
  } catch {
    throw new Error("invalid json");
  }
  let records: unknown[];
  if (!rule.record_path) records = [root];
  else {
    const recordPath = rule.record_path;
    const [vals, exists] = evalPath(root, recordPath);
    if (!exists) throw new Error("record_path matched nothing");
    if (vals.length === 0) return [];
    records = vals.length === 1 && Array.isArray(vals[0]) ? vals[0] : vals;
    if (records.length === 0) return [];
  }
  const recordPath = rule.record_path ?? "";
  return project(rule, records.length, (index, field) => {
    const found = lookupJSON(root, records[index], recordPath, field.from, index, records.length);
    if (!found.ok) return [undefined, false];
    return [found.value, true];
  });
}

function parseHTML(rule: Rule, body: string): Row[] {
  const doc = parseHTMLDocument(body);
  const nodes = rule.record_path ? doc.querySelectorAll(rule.record_path) : [doc];
  if (rule.record_path && nodes.length === 0) throw new Error("record_path matched nothing");
  return project(rule, nodes.length, (index, field) => {
    if (classify(field.from) !== "css") throw new Error("html fields use css selectors");
    const scope = nodes[index];
    if (!scope) return [undefined, false];
    const found = scope.querySelector(field.from);
    if (!found) return [undefined, false];
    if (field.attr) {
      const value = found.getAttribute(field.attr);
      return value == null ? [undefined, false] : [value, true];
    }
    return [found.text.trim(), true];
  });
}

function parseText(rule: Rule, body: string): Row[] {
  let chunks = [body];
  if (rule.record_path) {
    if (rule.record_path.startsWith("$")) throw new Error("text rules cannot use jsonpath record_path");
    let re: RegExp;
    try {
      re = compileNamed(rule.record_path);
    } catch {
      re = new RegExp(rule.record_path, "g");
    }
    const found = body.match(new RegExp(re.source, "g"));
    if (!found?.length) throw new Error("record_path matched nothing");
    chunks = found;
  }
  return project(rule, chunks.length, (index, field) => {
    const re = compileNamed(field.from);
    const chunk = chunks[index] ?? "";
    const match = re.exec(chunk);
    if (!match?.groups) return [undefined, false];
    const value = Object.values(match.groups)[0];
    return value == null ? [undefined, false] : [value, true];
  });
}

function project(rule: Rule, count: number, extract: (index: number, field: Field) => [unknown, boolean]): Row[] {
  const rows: Row[] = [];
  let lastError = "";
  for (let i = 0; i < count; i++) {
    const row: Row = {};
    let failed = false;
    for (const field of rule.fields) {
      const [raw, found] = extract(i, field);
      if (!found) {
        if (field.required) {
          failed = true;
          lastError = `required field ${field.name} is missing`;
        }
        row[field.name] = null;
        continue;
      }
      const [value, ok] = coerce(field.type, raw);
      if (!ok) {
        row[field.name] = null;
        continue;
      }
      if (field.required && blank(value)) {
        failed = true;
        lastError = `required field ${field.name} is empty`;
        row[field.name] = null;
        continue;
      }
      row[field.name] = value;
    }
    if (!failed) rows.push(row);
  }
  if (count > 0 && rows.length === 0) throw new Error(lastError || "parser produced no rows");
  return rows;
}

function blank(value: unknown): boolean {
  return value == null || (typeof value === "string" && value.trim() === "");
}

function coerce(type: Field["type"], raw: unknown): [unknown, boolean] {
  if (raw == null) return [null, false];
  if (type === "string") {
    if (typeof raw === "string") return [raw, true];
    if (typeof raw === "number" || typeof raw === "boolean") return [String(raw), true];
    try {
      return [JSON.stringify(raw), true];
    } catch {
      return [null, false];
    }
  }
  if (type === "number") {
    if (typeof raw === "number" && Number.isFinite(raw)) return [raw, true];
    if (typeof raw === "string" && raw.trim() !== "" && Number.isFinite(Number(raw))) return [Number(raw), true];
    if (typeof raw === "boolean") return [raw ? 1 : 0, true];
    return [null, false];
  }
  if (type === "boolean") {
    if (typeof raw === "boolean") return [raw, true];
    if (typeof raw === "number") return [raw !== 0, true];
    if (typeof raw === "string") {
      if (raw === "true" || raw === "1") return [true, true];
      if (raw === "false" || raw === "0") return [false, true];
    }
    return [null, false];
  }
  const text = String(raw).trim();
  if (!text) return [null, false];
  if (/^\d+$/.test(text) || !Number.isNaN(Date.parse(text))) return [text, true];
  return [null, false];
}

type Seg = { kind: "key"; key: string } | { kind: "index"; index: number } | { kind: "wild" };

function lookupJSON(
  root: unknown,
  record: unknown,
  recordPath: string,
  from: string,
  index: number,
  count: number,
): { ok: boolean; value?: unknown } {
  const pick = (node: unknown, path: string): unknown | null => {
    const [vals, exists] = evalPath(node, path);
    if (!exists || vals.length === 0 || vals[0] == null || vals[0] === "") return null;
    return vals.length === 1 ? vals[0] : vals;
  };
  let value = pick(record, from);
  if (value == null) {
    const rel = relativeToRecord(recordPath, from);
    if (rel) value = pick(record, rel);
  }
  if (value == null && recordPath) {
    const [vals, exists] = evalPath(root, from);
    if (exists && vals.length > 0) {
      if (vals.length === count && index < vals.length) value = vals[index];
      else if (count === 1) value = vals.length === 1 ? vals[0] : vals;
    }
  }
  return value == null ? { ok: false } : { ok: true, value };
}

function relativeToRecord(recordPath: string, from: string): string {
  if (!recordPath || !from.startsWith("$.")) return "";
  const base = recordPath.endsWith("[*]") ? recordPath.slice(0, -3) : recordPath;
  if (from.startsWith(recordPath + ".")) return "$." + from.slice(recordPath.length + 1);
  if (from.startsWith(base + ".")) {
    let rest = from.slice(base.length + 1);
    if (rest.startsWith("[*].")) rest = rest.slice(4);
    return "$." + rest;
  }
  return "";
}

function evalPath(root: unknown, path: string): [unknown[], boolean] {
  if (!path.startsWith("$")) throw new Error("jsonpath must start with $");
  return walk(root, parsePath(path));
}

function walk(item: unknown, segs: Seg[]): [unknown[], boolean] {
  if (segs.length === 0) return [[item], true];
  const [head, ...rest] = segs;
  if (!head) return [[item], true];
  if (head.kind === "key") {
    if (!item || typeof item !== "object" || Array.isArray(item) || !(head.key in (item as Record<string, unknown>))) {
      return [[], false];
    }
    return walk((item as Record<string, unknown>)[head.key], rest);
  }
  if (head.kind === "index") {
    if (!Array.isArray(item) || head.index < 0 || head.index >= item.length) return [[], false];
    return walk(item[head.index], rest);
  }
  if (!Array.isArray(item)) return [[], false];
  if (rest.length === 0) return [item, true];
  const all: unknown[] = [];
  let exists = false;
  for (const el of item) {
    const [vals, ok] = walk(el, rest);
    if (ok) {
      exists = true;
      all.push(...vals);
    }
  }
  return [all, exists];
}

function parsePath(path: string): Seg[] {
  if (path === "$") return [];
  const segs: Seg[] = [];
  let rest = path.slice(1);
  while (rest) {
    if (rest[0] === ".") {
      rest = rest.slice(1);
      const match = /^[A-Za-z_][A-Za-z0-9_]*/.exec(rest);
      if (!match) throw new Error("invalid jsonpath");
      segs.push({ kind: "key", key: match[0] });
      rest = rest.slice(match[0].length);
      continue;
    }
    if (rest[0] === "[") {
      const end = rest.indexOf("]");
      if (end < 0) throw new Error("invalid jsonpath");
      const inner = rest.slice(1, end);
      if (inner === "*") segs.push({ kind: "wild" });
      else if ((inner.startsWith("'") && inner.endsWith("'")) || (inner.startsWith('"') && inner.endsWith('"'))) {
        segs.push({ kind: "key", key: inner.slice(1, -1) });
      } else if (/^\d+$/.test(inner)) segs.push({ kind: "index", index: Number(inner) });
      else throw new Error("invalid jsonpath");
      rest = rest.slice(end + 1);
      continue;
    }
    throw new Error("invalid jsonpath");
  }
  return segs;
}
