import { matchesAny } from "../lib/match";
import { clampBodyLimit, stripSensitiveHeaders, truncateUtf8 } from "../lib/body";

interface CapturePayload {
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

const SOURCE = "edf-page";
const CONFIG_SOURCE = "edf-ext";

interface PageConfig {
  enabled: boolean;
  patterns: string[];
  excludes: string[];
  captureErrors: boolean;
  includeResponseHeaders: boolean;
  maxBodyBytes: number;
}

const config: PageConfig = {
  enabled: false,
  patterns: [],
  excludes: [],
  captureErrors: false,
  includeResponseHeaders: false,
  maxBodyBytes: 2 * 1024 * 1024,
};

export default defineContentScript({
  matches: ["<all_urls>"],
  allFrames: true,
  runAt: "document_start",
  world: "MAIN",
  main() {
    window.addEventListener("message", (event) => {
      if (event.source !== window) return;
      const data = event.data as { source?: string; type?: string; config?: PageConfig } | null;
      if (!data || data.source !== CONFIG_SOURCE || data.type !== "config" || !data.config) return;
      config.enabled = Boolean(data.config.enabled);
      config.patterns = Array.isArray(data.config.patterns) ? data.config.patterns : [];
      config.excludes = Array.isArray(data.config.excludes) ? data.config.excludes : [];
      config.captureErrors = Boolean(data.config.captureErrors);
      config.includeResponseHeaders = Boolean(data.config.includeResponseHeaders);
      config.maxBodyBytes = clampBodyLimit(data.config.maxBodyBytes);
    });

    const originalFetch = window.fetch.bind(window);
    window.fetch = async function (input: RequestInfo | URL, init?: RequestInit) {
      const response = await originalFetch(input, init);
      void readFetch(response, requestMethod(input, init)).catch(() => undefined);
      return response;
    };

    const originalOpen = XMLHttpRequest.prototype.open;
    const originalSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open = function (method: string, url: string | URL, async?: boolean, username?: string | null, password?: string | null) {
      (this as XMLHttpRequest & { __edfMethod?: string }).__edfMethod = String(method);
      return originalOpen.call(this, method, url, async ?? true, username, password);
    };
    XMLHttpRequest.prototype.send = function (body?: Document | XMLHttpRequestBodyInit | null) {
      this.addEventListener("load", () => {
        try {
          readXHR(this);
        } catch {
          // Ignore pages that block responseText for non-text response types.
        }
      });
      return originalSend.call(this, body);
    };
  },
});

async function readFetch(response: Response, method: string): Promise<void> {
  const url = response.url;
  if (!shouldCapture(url, response.status)) return;
  const contentType = response.headers.get("content-type") ?? "";
  if (!isTextual(contentType)) return;
  const text = await response.clone().text();
  emit(url, method, response.status, contentType, text, headerObject(response.headers));
}

function requestMethod(input: RequestInfo | URL, init?: RequestInit): string {
  if (init?.method) return init.method;
  if (typeof Request !== "undefined" && input instanceof Request) return input.method;
  return "GET";
}

function readXHR(xhr: XMLHttpRequest): void {
  const url = xhr.responseURL;
  const method = (xhr as XMLHttpRequest & { __edfMethod?: string }).__edfMethod || "GET";
  if (!shouldCapture(url, xhr.status)) return;
  const contentType = xhr.getResponseHeader("content-type") ?? "";
  if (!isTextual(contentType)) return;
  let text = "";
  if (xhr.responseType === "" || xhr.responseType === "text") text = xhr.responseText;
  else if (xhr.responseType === "json") text = JSON.stringify(xhr.response);
  else return;
  const headers: Record<string, string> = {};
  if (config.includeResponseHeaders) {
    for (const line of xhr.getAllResponseHeaders().trim().split(/[\r\n]+/)) {
      const idx = line.indexOf(":");
      if (idx <= 0) continue;
      headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
    }
  }
  emit(url, method, xhr.status, contentType, text, headers);
}

function emit(url: string, method: string, status: number, contentType: string, text: string, headers: Record<string, string>): void {
  const clipped = truncateUtf8(text, config.maxBodyBytes);
  const payload: CapturePayload = {
    protocol: 1,
    url,
    method,
    status,
    content_type: contentType,
    body: clipped.text,
    captured_at: new Date().toISOString(),
    truncated: clipped.truncated,
  };
  if (config.includeResponseHeaders) payload.response_headers = stripSensitiveHeaders(headers);
  window.postMessage({ source: SOURCE, type: "capture", payload }, "*");
}

function shouldCapture(url: string, status: number): boolean {
  if (!config.enabled || !url || !matchesAny(config.patterns, url) || matchesAny(config.excludes, url)) return false;
  if (status >= 200 && status < 300) return true;
  return config.captureErrors && status >= 400;
}

function isTextual(contentType: string): boolean {
  const ct = contentType.toLowerCase();
  return ct.includes("json") || ct.includes("html") || ct.includes("xml") || ct.includes("csv") || ct.includes("text/");
}

function headerObject(headers: Headers): Record<string, string> {
  if (!config.includeResponseHeaders) return {};
  const out: Record<string, string> = {};
  headers.forEach((value, key) => {
    out[key] = value;
  });
  return out;
}
