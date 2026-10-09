const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function matchPattern(pattern: string, rawURL: string): boolean {
  if (!pattern || /[\r\n]/.test(pattern)) return false;
  const parts = pattern.split("*").map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  return new RegExp(`^${parts.join(".*")}$`).test(rawURL);
}

export function literalPrefix(pattern: string): number {
  const i = pattern.indexOf("*");
  return i < 0 ? pattern.length : i;
}

export function literalCount(pattern: string): number {
  return pattern.length - (pattern.match(/\*/g)?.length ?? 0);
}

export interface Candidate {
  pattern: string;
  contentTypes?: string[];
  updatedAt: string;
}

export function selectRule(rules: Candidate[], rawURL: string, contentType: string): number {
  let best = -1;
  for (let i = 0; i < rules.length; i++) {
    const rule = rules[i];
    if (!rule || !matchPattern(rule.pattern, rawURL) || !contentTypeAllowed(rule.contentTypes, contentType)) continue;
    if (best < 0 || moreSpecific(rule, rules[best]!)) best = i;
  }
  return best;
}

function moreSpecific(a: Candidate, b: Candidate): boolean {
  const ap = literalPrefix(a.pattern);
  const bp = literalPrefix(b.pattern);
  if (ap !== bp) return ap > bp;
  const al = literalCount(a.pattern);
  const bl = literalCount(b.pattern);
  if (al !== bl) return al > bl;
  return a.updatedAt > b.updatedAt;
}

export function textual(contentType: string): boolean {
  const ct = baseType(contentType);
  if (!ct) return false;
  return ct.includes("json") || ct.includes("html") || ct.includes("xml") || ct.includes("csv") || ct.startsWith("text/");
}

export function kind(contentType: string): "json" | "html" | "text" {
  const ct = baseType(contentType);
  if (ct.includes("json")) return "json";
  if (ct.includes("html")) return "html";
  return "text";
}

function contentTypeAllowed(allowed: string[] | undefined, actual: string): boolean {
  if (!allowed || allowed.length === 0) return true;
  const base = baseType(actual);
  if (!base) return false;
  return allowed.some((item) => {
    const want = baseType(item);
    return want === base || base.includes(want) || want.includes(base);
  });
}

function baseType(value: string): string {
  return value.toLowerCase().split(";", 1)[0]?.trim() ?? "";
}

export function normalizeURL(rawURL: string): string {
  const url = new URL(rawURL);
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("url must be http(s)");
  const parts = url.pathname.split("/").map((part) => {
    if (!part) return part;
    if (/^[0-9]+$/.test(part) || UUID_RE.test(part)) return "*";
    return part;
  });
  let path = parts.join("/").replace(/\*\*/g, "*");
  if (url.search && !path.endsWith("*")) path += "*";
  return `${url.protocol}//${url.host}${path}`.replace(/\*\*/g, "*");
}
