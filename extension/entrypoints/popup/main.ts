import "./style.css";
import { settingsItem, readLocale, type Locale } from "../../lib/settings";
import { t } from "../../lib/i18n";

interface Status {
  enabled: boolean;
  serverUrl: string;
  reachable: boolean;
  detail: string;
  ok: number;
  fail: number;
  lastError: string;
  queued: number;
  uploadCount: number;
  lastUploadAt: string;
}

const app = document.querySelector<HTMLElement>("#app");
let locale: Locale = "en";

async function render(): Promise<void> {
  const [status, settings] = await Promise.all([
    browser.runtime.sendMessage({ type: "status" }) as Promise<Status>,
    settingsItem.getValue(),
  ]);
  if (!app || !status) return;
  locale = readLocale(settings);
  const reach = status.reachable ? t(locale, "reachable") : status.detail || t(locale, "unreachable");
  app.innerHTML = `
    <div class="head">
      <h1>${t(locale, "popupTitle")}</h1>
      <button id="lang" type="button">${t(locale, "switchTo")}</button>
    </div>
    <div class="row"><span>${t(locale, "capture")}</span><strong class="${status.enabled ? "ok" : "bad"}">${status.enabled ? t(locale, "on") : t(locale, "paused")}</strong></div>
    <div class="row"><span>${t(locale, "server")}</span><strong class="${status.reachable ? "ok" : "bad"}">${escapeHTML(reach)}</strong></div>
    <div class="stat"><span>${t(locale, "today")}</span><span>${t(locale, "todayStats", { ok: status.ok, fail: status.fail })}</span></div>
    <div class="stat"><span>${t(locale, "queued")}</span><span>${status.queued}</span></div>
    <div class="stat"><span>${t(locale, "uploads")}</span><span>${Number.isFinite(status.uploadCount) ? status.uploadCount : 0}</span></div>
    <div class="stat"><span>${t(locale, "lastUpload")}</span><span>${escapeHTML(formatUploadTime(status.lastUploadAt || "", locale))}</span></div>
    <p>${status.lastError ? escapeHTML(t(locale, "lastError", { error: status.lastError })) : t(locale, "noError")}</p>
    <p><a href="#" id="options">${t(locale, "openSettings")}</a></p>
  `;
  document.querySelector("#lang")?.addEventListener("click", () => {
    void switchLocale(locale === "zh" ? "en" : "zh");
  });
  document.querySelector("#options")?.addEventListener("click", (event) => {
    event.preventDefault();
    void browser.runtime.openOptionsPage();
  });
}

async function switchLocale(next: Locale): Promise<void> {
  const settings = await settingsItem.getValue();
  await settingsItem.setValue({ ...settings, locale: next });
  await render();
}

function formatUploadTime(iso: string, locale: Locale): string {
  if (!iso) return t(locale, "never");
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return t(locale, "never");
  return date.toLocaleString(locale === "zh" ? "zh-CN" : "en");
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch] ?? ch);
}

void render();
setInterval(() => void render(), 3000);
