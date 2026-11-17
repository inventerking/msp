import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { TOTP } from "otpauth";
import { admin, client, composeDir, dotenv, password, register, stamp, latestLink, escapedClient, freshTotpWindow } from "./helpers";

test("an admin activates a module and the workspace updates, then suspension blocks it at the API", async ({ browser }) => {
  const adminEmail = `e2e-admin-${stamp}@example.test`;
  if (composeDir) {
    execFileSync("docker", ["compose", "exec", "-T", "-e", `ADMIN_PASSWORD=${password}`, "api", "/engine", "create-admin", "--email", adminEmail, "--name", "E2E Admin"], { cwd: composeDir, stdio: "ignore" });
  } else {
    execFileSync("go", ["run", "./cmd/engine", "create-admin", "--email", adminEmail, "--name", "E2E Admin"], {
      cwd: resolve(import.meta.dirname, "../../../backend"),
      env: { ...process.env, ...dotenv(), ADMIN_PASSWORD: password },
      stdio: "ignore",
    });
  }

  // A new customer registers, verifies by email and creates a workspace.
  const customer = await (await browser.newContext()).newPage();
  const tenantName = `E2E Workspace ${stamp}`;
  await register(customer, `e2e-owner-${stamp}@example.test`, "Erin Owner");
  await expect(customer.getByRole("heading", { name: "Create a workspace" })).toBeVisible();
  await customer.getByLabel("Workspace name").fill(tenantName);
  await customer.getByRole("button", { name: "Create workspace" }).click();
  await expect(customer.getByRole("heading", { name: "Welcome, Erin Owner" })).toBeVisible();

  // Sales Intelligence is not active: the page explains why, and the API itself refuses.
  await customer.getByRole("navigation").getByRole("link", { name: "Sales Intelligence" }).click();
  await expect(customer.getByText("This module is not part of your workspace yet")).toBeVisible();
  const before = await customer.evaluate(async () => (await fetch("/api/v1/sales-intelligence/status")).status);
  expect(before).toBe(403);

  // An admin signs in and must enrol in MFA before anything else works.
  const console_ = await (await browser.newContext()).newPage();
  await console_.goto(`${admin}/login`);
  await console_.getByLabel("Email").fill(adminEmail);
  await console_.getByLabel("Password").fill(password);
  await console_.getByRole("button", { name: "Continue" }).click();
  await expect(console_.getByRole("heading", { name: "Set up two-step verification" })).toBeVisible();
  const secret = await console_.getByTestId("mfa-secret").innerText();
  await console_.getByLabel("Code").fill(new TOTP({ secret }).generate());
  await console_.getByRole("button", { name: "Verify" }).click();
  await expect(console_.getByRole("heading", { name: "Dashboard" })).toBeVisible();

  // The admin activates Sales Intelligence for this tenant, giving a reason.
  await console_.getByRole("navigation").getByRole("link", { name: "Tenants" }).click();
  await console_.getByRole("link", { name: tenantName }).click();
  const moduleRow = console_.locator('tr[data-module="sales_intelligence"]');
  await moduleRow.getByRole("button", { name: "Activate" }).click();
  await console_.getByRole("dialog").getByLabel("Reason").fill("E2E activation");
  await console_.getByRole("dialog").getByRole("button", { name: "Activate" }).click();
  await expect(moduleRow.getByText("Active", { exact: true })).toBeVisible();

  // The customer's open page updates by itself, with no reload.
  await expect(customer.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Definitions" })).toBeVisible({ timeout: 15_000 });
  const during = await customer.evaluate(async () => (await fetch("/api/v1/sales-intelligence/status")).status);
  expect(during).toBe(200);

  // Suspension stops access at the backend, and the page says so.
  await moduleRow.getByRole("button", { name: "Suspend" }).click();
  await console_.getByRole("dialog").getByLabel("Reason").fill("E2E suspension");
  await console_.getByRole("dialog").getByRole("button", { name: "Suspend" }).click();
  await expect(moduleRow.getByText("Suspended", { exact: true })).toBeVisible();
  await expect(customer.getByText("This module is suspended")).toBeVisible({ timeout: 15_000 });
  const after = await customer.evaluate(async () => {
    const r = await fetch("/api/v1/sales-intelligence/status");
    return { status: r.status, body: (await r.json()) as { code: string; module_status: string } };
  });
  expect(after.status).toBe(403);
  expect(after.body.code).toBe("module_not_entitled");
  expect(after.body.module_status).toBe("suspended");

  // The action is on the record, attributed to a platform administrator.
  await customer.getByRole("navigation").getByRole("link", { name: "Settings" }).click();
  await expect(customer.getByText("entitlement.suspend")).toBeVisible();
  await expect(customer.getByRole("row", { name: /entitlement\.suspend/ })).toContainText("Platform administrator");

  await customer.screenshot({ path: "test-results/customer-settings.png", fullPage: true });
  await console_.screenshot({ path: "test-results/admin-tenant.png", fullPage: true });
});

test("a removed member loses access, and a signed-in user is held at MFA", async ({ browser }) => {
  const cspViolations: string[] = [];
  const owner = await (await browser.newContext()).newPage();
  owner.on("console", (m) => {
    if (/Content Security Policy/i.test(m.text())) cspViolations.push(m.text());
  });
  await register(owner, `e2e-team-${stamp}@example.test`, "Tess Owner");
  await owner.getByLabel("Workspace name").fill(`E2E Team ${stamp}`);
  await owner.getByRole("button", { name: "Create workspace" }).click();
  await expect(owner.getByRole("heading", { name: "Welcome, Tess Owner" })).toBeVisible();

  // Turn on two-step verification from the account page.
  await owner.getByRole("link", { name: "Tess Owner" }).click();
  await owner.getByRole("button", { name: "Turn on" }).click();
  const secret = await owner.getByTestId("mfa-secret").innerText();
  // The previous step's code, so the sign-in below uses a different one and is not a replay.
  await freshTotpWindow();
  await owner.getByLabel("Code").fill(new TOTP({ secret }).generate({ timestamp: Date.now() - 30_000 }));
  await owner.getByRole("button", { name: "Confirm" }).click();
  await expect(owner.getByText("Save your recovery codes")).toBeVisible();
  await owner.getByRole("button", { name: "I have saved them" }).click();

  // A new browser with the right password cannot get past MFA.
  const second = await (await browser.newContext()).newPage();
  await second.goto(`${client}/login`);
  await second.getByLabel("Email").fill(`e2e-team-${stamp}@example.test`);
  await second.getByLabel("Password").fill(password);
  await second.getByRole("button", { name: "Sign in" }).click();
  await expect(second.getByRole("heading", { name: "Two-step verification" })).toBeVisible();
  await second.goto(`${client}/team`);
  await expect(second.getByRole("heading", { name: "Two-step verification" })).toBeVisible();
  await freshTotpWindow();
  await second.getByLabel("Code").fill(new TOTP({ secret }).generate());
  await second.getByRole("button", { name: "Verify" }).click();
  await expect(second.getByRole("heading", { name: /Welcome|Team/ })).toBeVisible();

  // Invite a teammate, who joins from the emailed link.
  const memberEmail = `e2e-member-${stamp}@example.test`;
  await owner.getByRole("navigation").getByRole("link", { name: "Team" }).click();
  await owner.getByLabel("Email").fill(memberEmail);
  await owner.getByRole("button", { name: "Invite member" }).click();
  await expect(owner.getByRole("cell", { name: memberEmail })).toBeVisible();
  const invite = await latestLink(memberEmail, new RegExp(`${escapedClient}\\/invite\\/[\\w.-]+`));
  const member = await (await browser.newContext()).newPage();
  await register(member, memberEmail, "Mo Member");
  await member.goto(invite);
  await member.getByRole("button", { name: "Accept invitation" }).click();
  await expect(member.getByRole("heading", { name: "Welcome, Mo Member" })).toBeVisible();

  // Nothing the app does may be blocked by the Content-Security-Policy on a deployed server.
  expect(cspViolations).toEqual([]);

  // Remove them. Their very next request is refused.
  await owner.reload();
  await owner.getByRole("row", { name: /Mo Member/ }).getByRole("button", { name: "Remove" }).click();
  await owner.getByRole("dialog").getByRole("button", { name: "Remove" }).click();
  await expect(owner.getByRole("row", { name: /Mo Member/ })).toHaveCount(0);
  const status = await member.evaluate(async () => (await fetch("/api/v1/tenant")).status);
  expect(status).toBe(403);
});
