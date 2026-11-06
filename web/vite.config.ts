import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The dashboard (index.html) and the embed page (embed.html) build into dist, which
// the deneb binary carries. `pnpm dev` proxies the API to a deneb on port 8080.
const api = "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rolldownOptions: { input: { index: "index.html", embed: "embed.html" } },
    // The player with hls.js is one chunk of about 900 kB (270 kB compressed), which the
    // embed page needs whole.
    chunkSizeWarningLimit: 1000,
  },
  server: {
    proxy: { "/v1": api, "/auth": api, "/play": api, "/embed/": api, "/healthz": api },
  },
});
