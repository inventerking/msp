import tailwind from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react(), tailwind()],
  server: {
    port: 5180,
    strictPort: true,
    // Same-origin API routing, matching production behind Traefik.
    proxy: { "/api": { target: "http://127.0.0.1:8088" } },
  },
  test: { environment: "jsdom", globals: true },
});
