export const PROTOCOL = 1;
export const MAX_BODY_BYTES = 1024 * 1024;

export interface Env {
  DB: D1Database;
  INGEST_TOKEN?: string;
  LLM_BASE_URL?: string;
  LLM_API_KEY?: string;
  LLM_MODEL?: string;
  LLM_TIMEOUT_SECONDS?: string;
  LLM_MAX_SAMPLE_CHARS?: string;
}

export interface Field {
  name: string;
  type: "string" | "number" | "boolean" | "timestamp";
  from: string;
  required?: boolean;
  attr?: string;
}

export interface Rule {
  pattern: string;
  content_types?: string[];
  record_path?: string;
  table: string;
  fields: Field[];
}

export interface StoredRule {
  id: string;
  pattern: string;
  rule: Rule;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface IngestRequest {
  url: string;
  method: string;
  status: number;
  content_type: string;
  body: string;
  captured_at: string;
  truncated: boolean;
}

export interface IngestResult {
  stored: true;
  mode: "parsed" | "raw";
  pattern?: string;
  table?: string;
  rows: number;
  llm: "skipped" | "scheduled" | "disabled";
}

export const SYSTEM_COLUMNS = ["_captured_at", "_source_url", "_rule_id", "_rule_version"] as const;
