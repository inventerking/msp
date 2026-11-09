import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig } from "@playwright/test";

// Runs against the real API, database and email sink. Needs `make up migrate` first.
// Set DEPLOYED_COMPOSE_DIR to test an already running testing server instead. See activation.spec.ts.
const deployed = Boolean(process.env.DEPLOYED_COMPOSE_DIR);
const env: Record<string, string> = {};
if (!deployed) {
  for (const line of readFileSync(resolve(import.meta.dirname, "../../.env"), "utf8").split("\n")) {
    const m = /^([A-Z0-9_]+)=(.*)$/.exec(line);
    if (m?.[1]) env[m[1]] = m[2] ?? "";
  }
}

// Repeated runs would otherwise hit the real sign-in rate limit, which is behaving as designed.
env.AUTH_RATE_PER_MINUTE = "100000";

export default defineConfig({
  testDir: "tests",
  timeout: 90_000,
  workers: 1,
  reporter: [["list"]],
  use: { screenshot: "only-on-failure", trace: "retain-on-failure", ignoreHTTPSErrors: deployed },
  webServer: deployed ? [] : [
    { command: "go run ./cmd/engine api", cwd: resolve(import.meta.dirname, "../../backend"), env, url: "http://127.0.0.1:8088/healthz", reuseExistingServer: true, timeout: 120_000, stdout: "ignore" },
    { command: "pnpm --filter @msp/client dev", cwd: resolve(import.meta.dirname, "../.."), url: "http://localhost:5180", reuseExistingServer: true },
    { command: "pnpm --filter @msp/admin dev", cwd: resolve(import.meta.dirname, "../.."), url: "http://localhost:5181", reuseExistingServer: true },
  ],
});
