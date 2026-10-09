export const DEFAULT_MAX_BODY_BYTES = 2 * 1024 * 1024;
export const HARD_MAX_BODY_BYTES = 10 * 1024 * 1024;

const SENSITIVE = new Set(["cookie", "set-cookie", "authorization"]);

export function clampBodyLimit(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return DEFAULT_MAX_BODY_BYTES;
  return Math.min(Math.floor(value), HARD_MAX_BODY_BYTES);
}

export const BODY_PRESETS = [
  { label: "64K", bytes: 64 * 1024 },
  { label: "128K", bytes: 128 * 1024 },
  { label: "256K", bytes: 256 * 1024 },
  { label: "512K", bytes: 512 * 1024 },
  { label: "1MB", bytes: 1024 * 1024 },
  { label: "2MB", bytes: 2 * 1024 * 1024 },
  { label: "4MB", bytes: 4 * 1024 * 1024 },
  { label: "8MB", bytes: 8 * 1024 * 1024 },
  { label: "10MB", bytes: 10 * 1024 * 1024 },
] as const;

// parseBodyLimit accepts a byte count or a size with k/kb/m/mb. Units use 1024.
export function parseBodyLimit(raw: string): number | null {
  const text = raw.trim().replace(/,/g, "");
  const match = text.match(/^(\d+(?:\.\d+)?)\s*(kb|k|mb|m)?$/i);
  if (!match) return null;
  const amount = Number(match[1]);
  if (!Number.isFinite(amount) || amount <= 0) return null;
  const unit = (match[2] || "").toLowerCase();
  const factor = unit === "m" || unit === "mb" ? 1024 * 1024 : unit === "k" || unit === "kb" ? 1024 : 1;
  return clampBodyLimit(amount * factor);
}

export function stripSensitiveHeaders(headers: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers)) {
    if (SENSITIVE.has(key.toLowerCase())) continue;
    out[key] = value;
  }
  return out;
}

export function truncateUtf8(text: string, maxBytes: number): { text: string; truncated: boolean } {
  const bytes = new TextEncoder().encode(text);
  if (bytes.length <= maxBytes) return { text, truncated: false };
  let end = maxBytes;
  while (end > 0 && (bytes[end]! & 0xc0) === 0x80) end--;
  return { text: new TextDecoder().decode(bytes.slice(0, end)), truncated: true };
}
