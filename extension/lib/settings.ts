import { storage } from "wxt/utils/storage";
import { clampBodyLimit, DEFAULT_MAX_BODY_BYTES } from "./body";
import { normalizeLocale, type Locale } from "./i18n";

export { clampBodyLimit, stripSensitiveHeaders, truncateUtf8 } from "./body";
export { normalizeLocale, type Locale } from "./i18n";
export const DEFAULT_SERVER_URL = "http://127.0.0.1:8080";
export const MAX_QUEUE_ITEMS = 100;
export const MAX_QUEUE_BYTES = 20 * 1024 * 1024;

export type PatternMode = "include" | "exclude";

export interface PatternRule {
  id: string;
  pattern: string;
  enabled: boolean;
  mode: PatternMode;
}

export interface Settings {
  enabled: boolean;
  serverUrl: string;
  token: string;
  captureErrors: boolean;
  includeResponseHeaders: boolean;
  maxBodyBytes: number;
  locale: Locale;
  patterns: PatternRule[];
}

export interface IngestPayload {
  protocol: 1;
  url: string;
  method: string;
  status: number;
  content_type: string;
  body: string;
  captured_at: string;
  truncated: boolean;
  response_headers?: Record<string, string>;
}

export interface QueueItem {
  id: string;
  enqueuedAt: number;
  nextAttemptAt: number;
  attempts: number;
  bytes: number;
  payload: IngestPayload;
}

export interface DayStats {
  day: string;
  ok: number;
  fail: number;
  lastError: string;
}

export interface UploadStats {
  count: number;
  lastAt: string;
}

export const defaultSettings = (): Settings => ({
  enabled: true,
  serverUrl: DEFAULT_SERVER_URL,
  token: "",
  captureErrors: false,
  includeResponseHeaders: false,
  maxBodyBytes: DEFAULT_MAX_BODY_BYTES,
  locale: "en",
  patterns: [],
});

export const settingsItem = storage.defineItem<Settings>("local:settings", {
  fallback: defaultSettings(),
});

export const queueItem = storage.defineItem<QueueItem[]>("local:queue", {
  fallback: [],
});

export const statsItem = storage.defineItem<DayStats>("local:stats", {
  fallback: { day: "", ok: 0, fail: 0, lastError: "" },
});

export const uploadsItem = storage.defineItem<UploadStats>("local:uploads", {
  fallback: { count: 0, lastAt: "" },
});

export function readUploads(value: Partial<UploadStats> | null | undefined): UploadStats {
  const count = Number(value?.count);
  const lastAt = typeof value?.lastAt === "string" ? value.lastAt : "";
  return {
    count: Number.isFinite(count) && count > 0 ? Math.floor(count) : 0,
    lastAt: Number.isNaN(Date.parse(lastAt)) ? "" : lastAt,
  };
}

export function todayKey(date = new Date()): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

export function readLocale(settings: Partial<Settings> | null | undefined): Locale {
  return normalizeLocale(settings?.locale);
}

export function isLocalServer(serverUrl: string): boolean {
  try {
    const url = new URL(serverUrl);
    return url.hostname === "localhost" || url.hostname === "127.0.0.1" || url.hostname === "::1";
  } catch {
    return false;
  }
}
