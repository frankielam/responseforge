import { captureAllowed } from "../lib/match";
import {
  MAX_QUEUE_BYTES,
  MAX_QUEUE_ITEMS,
  clampBodyLimit,
  queueItem,
  settingsItem,
  statsItem,
  uploadsItem,
  stripSensitiveHeaders,
  todayKey,
  truncateUtf8,
  readUploads,
  type IngestPayload,
  type QueueItem,
} from "../lib/settings";

export default defineBackground(() => {
  browser.runtime.onMessage.addListener((message, _sender, sendResponse) => {
    const data = message as { type?: string; payload?: unknown };
    if (data?.type === "capture" && data.payload) {
      void enqueue(data.payload)
        .then(() => flush())
        .catch(() => undefined);
      return false;
    }
    if (data?.type === "status") {
      void flush().then(() => status()).then(sendResponse);
      return true;
    }
    return false;
  });

  browser.alarms.create("edf-flush", { periodInMinutes: 1 });
  browser.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name === "edf-flush") void flush();
  });
  void flush();
});

async function enqueue(value: unknown): Promise<void> {
  if (!isCapturePayload(value)) return;
  const raw = value;
  const settings = await settingsItem.getValue();
  if (!settings.enabled) return;
  if (!raw.url || !captureAllowed(settings.patterns, raw.url)) return;
  const limit = clampBodyLimit(settings.maxBodyBytes);
  const clipped = truncateUtf8(String(raw.body ?? ""), limit);
  const payload: IngestPayload = {
    protocol: 1,
    url: raw.url,
    method: String(raw.method || "GET"),
    status: Number(raw.status),
    content_type: String(raw.content_type || ""),
    body: clipped.text,
    captured_at: raw.captured_at || new Date().toISOString(),
    truncated: Boolean(raw.truncated) || clipped.truncated,
  };
  if (settings.includeResponseHeaders && raw.response_headers) {
    payload.response_headers = stripSensitiveHeaders(raw.response_headers);
  }
  const bytes = new TextEncoder().encode(JSON.stringify(payload)).length;
  const queue = await queueItem.getValue();
  queue.push({
    id: crypto.randomUUID(),
    enqueuedAt: Date.now(),
    nextAttemptAt: Date.now(),
    attempts: 0,
    bytes,
    payload,
  });
  await queueItem.setValue(trimQueue(queue));
}

function isCapturePayload(value: unknown): value is IngestPayload {
  if (!value || typeof value !== "object") return false;
  const payload = value as Partial<IngestPayload>;
  if (payload.protocol !== 1 || typeof payload.url !== "string" || typeof payload.method !== "string") return false;
  if (typeof payload.status !== "number" || !Number.isInteger(payload.status) || payload.status < 100 || payload.status > 599) return false;
  if (typeof payload.content_type !== "string" || typeof payload.body !== "string") return false;
  if (typeof payload.captured_at !== "string" || Number.isNaN(Date.parse(payload.captured_at))) return false;
  if (typeof payload.truncated !== "boolean") return false;
  try {
    const url = new URL(payload.url);
    if (url.protocol !== "http:" && url.protocol !== "https:") return false;
  } catch {
    return false;
  }
  return /^[A-Za-z]+$/.test(payload.method) && isTextual(payload.content_type);
}

function isTextual(contentType: string): boolean {
  const ct = contentType.toLowerCase();
  return ct.includes("json") || ct.includes("html") || ct.includes("xml") || ct.includes("csv") || ct.includes("text/");
}

function trimQueue(queue: QueueItem[]): QueueItem[] {
  const size = () => queue.reduce((sum, item) => sum + item.bytes, 0);
  while (queue.length > MAX_QUEUE_ITEMS || (queue.length > 0 && size() > MAX_QUEUE_BYTES)) {
    queue.shift();
  }
  return queue;
}

async function flush(): Promise<void> {
  const settings = await settingsItem.getValue();
  if (!settings.enabled) return;
  const queue = await queueItem.getValue();
  if (queue.length === 0) return;
  const remain: QueueItem[] = [];
  const base = settings.serverUrl.replace(/\/$/, "");
  for (const item of queue) {
    if (item.nextAttemptAt > Date.now()) {
      remain.push(item);
      continue;
    }
    const outcome = await post(base, settings.token, item.payload);
    if (outcome === "ok") {
      await bump(true, "");
      continue;
    }
    if (outcome === "drop") {
      await bump(false, "Server rejected the payload");
      continue;
    }
    item.attempts += 1;
    item.nextAttemptAt = Date.now() + Math.min(60_000, 1000 * 2 ** Math.min(item.attempts, 5));
    remain.push(item);
    await bump(false, outcome);
  }
  await queueItem.setValue(remain);
}

async function post(base: string, token: string, payload: IngestPayload): Promise<"ok" | "drop" | string> {
  try {
    const headers: Record<string, string> = {
      "content-type": "application/json",
      accept: "application/json; protocol=1",
    };
    if (token) headers.authorization = `Bearer ${token}`;
    const response = await fetch(`${base}/api/v1/ingest`, {
      method: "POST",
      headers,
      body: JSON.stringify(payload),
    });
    if (response.status === 202) return "ok";
    if (response.status === 400 || response.status === 413 || response.status === 415) return "drop";
    return `HTTP ${response.status}`;
  } catch {
    return "Server unreachable";
  }
}

async function bump(ok: boolean, error: string): Promise<void> {
  const day = todayKey();
  const stats = await statsItem.getValue();
  const current = stats.day === day ? stats : { day, ok: 0, fail: 0, lastError: "" };
  if (ok) {
    current.ok += 1;
    const uploads = readUploads(await uploadsItem.getValue());
    uploads.count += 1;
    uploads.lastAt = new Date().toISOString();
    await uploadsItem.setValue(uploads);
  } else {
    current.fail += 1;
    current.lastError = error;
  }
  await statsItem.setValue(current);
}

async function status() {
  const settings = await settingsItem.getValue();
  const queue = await queueItem.getValue();
  const stats = await statsItem.getValue();
  const day = todayKey();
  const today = stats.day === day ? stats : { day, ok: 0, fail: 0, lastError: "" };
  const uploads = readUploads(await uploadsItem.getValue());
  let reachable = false;
  let detail = "";
  try {
    const response = await fetch(`${settings.serverUrl.replace(/\/$/, "")}/healthz`);
    reachable = response.ok;
    if (!response.ok) detail = `HTTP ${response.status}`;
  } catch {
    detail = "Server unreachable";
  }
  return {
    enabled: settings.enabled,
    serverUrl: settings.serverUrl,
    reachable,
    detail,
    ok: today.ok,
    fail: today.fail,
    lastError: today.lastError,
    queued: queue.length,
    uploadCount: uploads.count,
    lastUploadAt: uploads.lastAt,
  };
}
