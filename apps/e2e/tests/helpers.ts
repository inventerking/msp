import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, type Browser, type Page } from "@playwright/test";
import { TOTP } from "otpauth";

// Defaults are the local stack. To test a running testing server, set DEPLOYED_COMPOSE_DIR and:
// CLIENT_URL, ADMIN_URL and MAILPIT_URL.
export const client = process.env.CLIENT_URL ?? "http://localhost:5180";
export const admin = process.env.ADMIN_URL ?? "http://localhost:5181";
export const mailpit = process.env.MAILPIT_URL ?? "http://127.0.0.1:58025";
export const composeDir = process.env.DEPLOYED_COMPOSE_DIR;
export const escapedClient = client.replace(/[.:/]/g, "\\$&");
export const password = "a-long-e2e-passphrase";
export const stamp = Date.now();

export function dotenv(): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of readFileSync(resolve(import.meta.dirname, "../../../.env"), "utf8").split("\n")) {
    const m = /^([A-Z0-9_]+)=(.*)$/.exec(line);
    if (m?.[1]) out[m[1]] = m[2] ?? "";
  }
  return out;
}

export async function latestLink(to: string, pattern: RegExp): Promise<string> {
  for (let i = 0; i < 150; i++) {
    const search = (await (await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`)).json()) as { messages?: { ID: string }[] };
    const id = search.messages?.[0]?.ID;
    if (id) {
      const msg = (await (await fetch(`${mailpit}/api/v1/message/${id}`)).json()) as { Text: string };
      const m = pattern.exec(msg.Text);
      if (m) return m[0];
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`no email to ${to}`);
}

export async function register(page: Page, email: string, name: string) {
  await page.goto(`${client}/register`);
  await page.getByLabel("Your name").fill(name);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Create an account" }).click();
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();
  await page.goto(await latestLink(email, new RegExp(`${escapedClient}\\/verify-email\\?token=[\\w-]+`)));
  await expect(page.getByRole("heading", { name: "Email confirmed" })).toBeVisible();
  await page.getByRole("link", { name: "Sign in" }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).not.toHaveURL(/\/login/);
}


/** Creates a platform admin and signs them in to the console, through MFA enrolment. */
export async function adminConsole(browser: Browser, email: string): Promise<Page> {
  if (composeDir) {
    execFileSync("docker", ["compose", "exec", "-T", "-e", `ADMIN_PASSWORD=${password}`, "api", "/engine", "create-admin", "--email", email, "--name", "E2E Admin"], { cwd: composeDir, stdio: "ignore" });
  } else {
    execFileSync("go", ["run", "./cmd/engine", "create-admin", "--email", email, "--name", "E2E Admin"], {
      cwd: resolve(import.meta.dirname, "../../../backend"),
      env: { ...process.env, ...dotenv(), ADMIN_PASSWORD: password },
      stdio: "ignore",
    });
  }
  const page = await (await browser.newContext()).newPage();
  await page.goto(`${admin}/login`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { name: "Set up two-step verification" })).toBeVisible();
  const secret = await page.getByTestId("mfa-secret").innerText();
  await page.getByLabel("Code").fill(new TOTP({ secret }).generate());
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  return page;
}

/** Waits until a TOTP step has at least 8 seconds left, so a code cannot expire between typing and submitting. */
export async function freshTotpWindow() {
  const left = 30 - ((Date.now() / 1000) % 30);
  if (left < 8) await new Promise((r) => setTimeout(r, left * 1000 + 250));
}
