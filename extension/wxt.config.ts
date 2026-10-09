import { defineConfig } from "wxt";

export default defineConfig({
  manifest: {
    name: "ResponseForge",
    description: "Turn matching fetch and XHR responses into structured data.",
    permissions: ["storage", "alarms"],
    host_permissions: ["<all_urls>"],
  },
});
