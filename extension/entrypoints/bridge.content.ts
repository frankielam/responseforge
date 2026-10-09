import { settingsItem } from "../lib/settings";

const PAGE_SOURCE = "edf-page";
const CONFIG_SOURCE = "edf-ext";

export default defineContentScript({
  matches: ["<all_urls>"],
  allFrames: true,
  runAt: "document_start",
  world: "ISOLATED",
  main() {
    const push = async () => {
      const settings = await settingsItem.getValue();
      const enabled = settings.patterns.filter((rule) => rule.enabled && rule.pattern);
      window.postMessage(
        {
          source: CONFIG_SOURCE,
          type: "config",
          config: {
            enabled: settings.enabled,
            patterns: enabled.filter((rule) => rule.mode !== "exclude").map((rule) => rule.pattern),
            excludes: enabled.filter((rule) => rule.mode === "exclude").map((rule) => rule.pattern),
            captureErrors: settings.captureErrors,
            includeResponseHeaders: settings.includeResponseHeaders,
            maxBodyBytes: settings.maxBodyBytes,
          },
        },
        "*",
      );
    };
    void push();
    settingsItem.watch(() => {
      void push();
    });
    window.addEventListener("message", (event) => {
      if (event.source !== window) return;
      const data = event.data as { source?: string; type?: string; payload?: unknown } | null;
      if (!data || data.source !== PAGE_SOURCE || data.type !== "capture" || !data.payload) return;
      void browser.runtime.sendMessage({ type: "capture", payload: data.payload });
    });
  },
});
