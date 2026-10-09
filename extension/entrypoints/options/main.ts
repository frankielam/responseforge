import "./style.css";
import { parseBodyLimit } from "../../lib/body";
import { t, type Locale } from "../../lib/i18n";
import { patternConflicts } from "../../lib/match";
import { defaultSettings, isLocalServer, readLocale, settingsItem, type PatternMode, type PatternRule, type Settings } from "../../lib/settings";

const enabled = document.querySelector<HTMLInputElement>("#enabled")!;
const serverDetails = document.querySelector<HTMLDetailsElement>("#serverDetails")!;
const serverSummary = document.querySelector<HTMLElement>("#serverSummary")!;
const serverUrl = document.querySelector<HTMLInputElement>("#serverUrl")!;
const token = document.querySelector<HTMLInputElement>("#token")!;
const captureErrors = document.querySelector<HTMLInputElement>("#captureErrors")!;
const includeHeaders = document.querySelector<HTMLInputElement>("#includeHeaders")!;
const maxBody = document.querySelector<HTMLInputElement>("#maxBody")!;
const patterns = document.querySelector<HTMLElement>("#patterns")!;
const emptyPatterns = document.querySelector<HTMLElement>("#emptyPatterns")!;
const newPattern = document.querySelector<HTMLInputElement>("#newPattern")!;
const newMode = document.querySelector<HTMLSelectElement>("#newMode")!;
const remote = document.querySelector<HTMLElement>("#remote")!;
const conflict = document.querySelector<HTMLElement>("#conflict")!;
const saved = document.querySelector<HTMLElement>("#saved")!;
const importFile = document.querySelector<HTMLInputElement>("#importFile")!;

let draft: Settings = defaultSettings();
let locale: Locale = "en";

function applyCopy(): void {
  document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
  document.title = t(locale, "title");
  document.querySelectorAll<HTMLElement>("[data-i18n]").forEach((node) => {
    const key = node.dataset.i18n as Parameters<typeof t>[1] | undefined;
    if (key) node.textContent = t(locale, key);
  });
  document.querySelectorAll<HTMLElement>("[data-i18n-html]").forEach((node) => {
    const key = node.dataset.i18nHtml as Parameters<typeof t>[1] | undefined;
    if (key) node.innerHTML = t(locale, key);
  });
  const lang = document.querySelector("#lang");
  if (lang) lang.textContent = t(locale, "switchTo");
  token.placeholder = t(locale, "tokenPlaceholder");
  newPattern.placeholder = t(locale, "patternPlaceholder");
  newMode.options[0]!.textContent = t(locale, "include");
  newMode.options[1]!.textContent = t(locale, "exclude");
  newMode.setAttribute("aria-label", t(locale, "patterns"));
}

function normalizeRule(rule: Partial<PatternRule>): PatternRule {
  return {
    id: rule.id || crypto.randomUUID(),
    pattern: String(rule.pattern ?? "").trim(),
    enabled: rule.enabled !== false,
    mode: rule.mode === "exclude" ? "exclude" : "include",
  };
}

function renderPatterns(): void {
  patterns.innerHTML = "";
  emptyPatterns.hidden = draft.patterns.length > 0;
  draft.patterns.forEach((rule, index) => {
    const row = document.createElement("div");
    row.className = rule.mode === "exclude" ? "pattern exclude" : "pattern";
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = rule.enabled;
    box.title = t(locale, "enabledTitle");
    box.addEventListener("change", () => {
      rule.enabled = box.checked;
      showConflicts();
    });
    const mode = document.createElement("select");
    mode.innerHTML = `<option value="include">${t(locale, "include")}</option><option value="exclude">${t(locale, "exclude")}</option>`;
    mode.value = rule.mode;
    mode.addEventListener("change", () => {
      rule.mode = mode.value === "exclude" ? "exclude" : "include";
      row.className = rule.mode === "exclude" ? "pattern exclude" : "pattern";
      showConflicts();
    });
    const input = document.createElement("input");
    input.type = "text";
    input.value = rule.pattern;
    input.spellcheck = false;
    input.addEventListener("input", () => {
      rule.pattern = input.value.trim();
      showConflicts();
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.textContent = t(locale, "remove");
    remove.addEventListener("click", () => {
      draft.patterns.splice(index, 1);
      renderPatterns();
      showConflicts();
    });
    row.append(box, mode, input, remove);
    patterns.append(row);
  });
}

function showRemote(): void {
  remote.hidden = isLocalServer(serverUrl.value);
  serverSummary.textContent = serverUrl.value.trim() || t(locale, "notSet");
}

function showConflicts(): void {
  const warnings = patternConflicts(draft.patterns).map((item) => t(locale, "conflict", { exclude: item.exclude, include: item.include }));
  conflict.hidden = warnings.length === 0;
  conflict.textContent = warnings.join("\n");
}

function syncPresets(): void {
  const parsed = parseBodyLimit(maxBody.value);
  document.querySelectorAll<HTMLButtonElement>(".presets button").forEach((button) => {
    button.setAttribute("aria-pressed", String(parsed === Number(button.dataset.bytes)));
  });
}

function currentSettings(): Settings {
  return {
    enabled: enabled.checked,
    serverUrl: serverUrl.value.trim() || defaultSettings().serverUrl,
    token: token.value,
    captureErrors: captureErrors.checked,
    includeResponseHeaders: includeHeaders.checked,
    maxBodyBytes: parseBodyLimit(maxBody.value) ?? draft.maxBodyBytes,
    locale,
    patterns: draft.patterns.map(normalizeRule).filter((rule) => rule.pattern),
  };
}

settingsItem.getValue().then((settings) => {
  draft = {
    ...defaultSettings(),
    ...settings,
    patterns: (settings.patterns ?? []).map((rule) => normalizeRule(rule)),
  };
  locale = readLocale(settings);
  enabled.checked = draft.enabled;
  serverUrl.value = draft.serverUrl;
  token.value = draft.token;
  captureErrors.checked = draft.captureErrors;
  includeHeaders.checked = draft.includeResponseHeaders;
  maxBody.value = String(draft.maxBodyBytes);
  serverDetails.open = !draft.serverUrl.trim();
  applyCopy();
  renderPatterns();
  showRemote();
  showConflicts();
  syncPresets();
});

serverUrl.addEventListener("input", showRemote);
document.querySelector("#add")?.addEventListener("click", () => {
  const pattern = newPattern.value.trim();
  if (!pattern) return;
  const mode: PatternMode = newMode.value === "exclude" ? "exclude" : "include";
  draft.patterns.push(normalizeRule({ pattern, enabled: true, mode }));
  newPattern.value = "";
  renderPatterns();
  showConflicts();
});

document.querySelector("#exportPatterns")?.addEventListener("click", () => {
  const body = JSON.stringify({ patterns: draft.patterns.map(normalizeRule).filter((rule) => rule.pattern) }, null, 2);
  const blob = new Blob([body + "\n"], { type: "application/json" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = "url-patterns.json";
  link.click();
  URL.revokeObjectURL(link.href);
  saved.textContent = t(locale, "exported");
});

document.querySelector("#importPatterns")?.addEventListener("click", () => importFile.click());
importFile.addEventListener("change", async () => {
  const file = importFile.files?.[0];
  importFile.value = "";
  if (!file) return;
  try {
    const parsed = JSON.parse(await file.text()) as { patterns?: unknown } | unknown[];
    const list = Array.isArray(parsed) ? parsed : parsed.patterns;
    if (!Array.isArray(list)) throw new Error(t(locale, "importShape"));
    draft.patterns = list.map((item) => normalizeRule(item as Partial<PatternRule>)).filter((rule) => rule.pattern);
    renderPatterns();
    showConflicts();
    saved.textContent = t(locale, "imported", { count: draft.patterns.length });
  } catch (error) {
    saved.textContent = error instanceof Error ? error.message : t(locale, "importFail");
  }
});

document.querySelector("#form")?.addEventListener("submit", async (event) => {
  event.preventDefault();
  const bytes = parseBodyLimit(maxBody.value);
  if (bytes == null) {
    saved.textContent = t(locale, "badBody");
    maxBody.focus();
    return;
  }
  maxBody.value = String(bytes);
  syncPresets();
  const next = currentSettings();
  draft = next;
  showConflicts();
  await settingsItem.setValue(next);
  const warnings = patternConflicts(next.patterns);
  saved.textContent = warnings.length ? t(locale, "savedWarn") : t(locale, "saved");
});

maxBody.addEventListener("input", syncPresets);
document.querySelectorAll<HTMLButtonElement>(".presets button").forEach((button) => {
  button.addEventListener("click", () => {
    maxBody.value = button.dataset.bytes || "";
    syncPresets();
  });
});

document.querySelector("#lang")?.addEventListener("click", async () => {
  locale = locale === "zh" ? "en" : "zh";
  applyCopy();
  renderPatterns();
  showRemote();
  showConflicts();
  const stored = await settingsItem.getValue();
  await settingsItem.setValue({ ...stored, locale });
});
