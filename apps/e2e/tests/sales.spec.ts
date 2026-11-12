import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { adminConsole, client, composeDir, dotenv, escapedClient, latestLink, register, stamp } from "./helpers";

let worker: ChildProcess | undefined;
test.beforeAll(() => {
  if (composeDir) return;
  const backend = resolve(import.meta.dirname, "../../../backend");
  const bin = join(tmpdir(), `msp-e2e-sales-${process.pid}`);
  execFileSync("go", ["build", "-o", bin, "./cmd/engine"], { cwd: backend, stdio: "ignore" });
  worker = spawn(bin, ["worker"], { cwd: backend, env: { ...process.env, ...dotenv() }, stdio: "ignore" });
});
test.afterAll(() => {
  worker?.kill("SIGTERM");
});

test("a deal moves through the pipeline, a proposal is sent and opened from its link, and an authorized assessment runs", async ({ browser }) => {
  test.setTimeout(240_000);
  const owner = await (await browser.newContext()).newPage();
  const tenantName = `E2E Sales ${stamp}`;
  await register(owner, `e2e-sales-${stamp}@example.test`, "Sam Seller");
  await owner.getByLabel("Workspace name").fill(tenantName);
  await owner.getByRole("button", { name: "Create workspace" }).click();
  await expect(owner.getByRole("heading", { name: "Welcome, Sam Seller" })).toBeVisible();

  const console_ = await adminConsole(browser, `e2e-admin-sales-${stamp}@example.test`);
  await console_.getByRole("navigation").getByRole("link", { name: "Tenants" }).click();
  await console_.getByRole("link", { name: tenantName }).click();
  for (const key of ["crm", "proposals", "it_assessment"]) {
    const row = console_.locator(`tr[data-module="${key}"]`);
    await row.getByRole("button", { name: "Activate" }).click();
    await console_.getByRole("dialog").getByLabel("Reason").fill("E2E sales");
    await console_.getByRole("dialog").getByRole("button", { name: "Activate" }).click();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
  }
  await owner.bringToFront();
  await owner.reload();

  // CRM: a company and a deal, moved along the pipeline from the keyboard-friendly select.
  await owner.getByRole("navigation").getByRole("link", { name: "CRM" }).click();
  await owner.getByRole("link", { name: "Companies" }).click();
  await owner.getByLabel("Company name").fill("Harbor Dental");
  await owner.getByLabel("Website").fill("harbor-dental-group.example");
  await owner.getByRole("button", { name: "Add company" }).click();
  await expect(owner.getByRole("link", { name: "Harbor Dental" })).toBeVisible();
  await owner.getByRole("link", { name: "Pipeline" }).click();
  await owner.getByLabel("Company", { exact: true }).selectOption({ label: "Harbor Dental" });
  await owner.getByLabel("Deal", { exact: true }).fill("Managed IT for Harbor");
  await owner.getByLabel("Monthly value").fill("2500");
  await owner.getByRole("button", { name: "Add deal" }).click();
  const newLead = owner.getByRole("region", { name: "New lead" });
  await expect(newLead.getByRole("link", { name: "Managed IT for Harbor" })).toBeVisible();
  await newLead.getByLabel("Move Managed IT for Harbor to").selectOption({ label: "Qualified" });
  await expect(owner.getByRole("region", { name: "Qualified" }).getByRole("link", { name: "Managed IT for Harbor" })).toBeVisible();

  // Proposals: priced on the server, sent, and opened by the customer without an account.
  await owner.getByRole("navigation").getByRole("link", { name: "Proposals" }).click();
  await owner.getByLabel("Title").fill("Harbor managed services");
  await owner.getByLabel("Customer").fill("Harbor Dental");
  await owner.getByRole("button", { name: "Create proposal" }).click();
  await expect(owner.getByRole("heading", { name: "Harbor managed services" })).toBeVisible();
  const valid = new Date(Date.now() + 30 * 86_400_000).toISOString().slice(0, 10);
  await owner.getByLabel("Valid until").fill(valid);
  await owner.getByRole("button", { name: "Add line" }).click();
  await owner.getByRole("textbox", { name: "Service" }).fill("Managed users");
  await owner.getByRole("spinbutton", { name: "Quantity" }).fill("10");
  await owner.getByRole("textbox", { name: "Unit price" }).fill("25");
  await owner.getByRole("button", { name: "Save draft" }).click();
  await expect(owner.getByText("£250.00").first()).toBeVisible();
  await expect(owner.getByText("£3,000.00")).toBeVisible();
  const recipient = `customer-${stamp}@example.test`;
  await owner.getByLabel("Recipient name").fill("Robin Customer");
  await owner.getByLabel("Recipient email").fill(recipient);
  await owner.getByRole("button", { name: "Send", exact: true }).click();
  await expect(owner.getByText("Sent", { exact: true }).first()).toBeVisible();
  const link = await latestLink(recipient, new RegExp(`${escapedClient}\\/p\\/[\\w-]+`));
  const customer = await (await browser.newContext()).newPage();
  await customer.goto(link);
  await expect(customer.getByRole("heading", { name: "Harbor managed services" })).toBeVisible();
  await expect(customer.getByText("£3,000.00")).toBeVisible();
  await expect(customer.getByText("All prices exclude tax.")).toBeVisible();
  await owner.bringToFront();
  await owner.reload();
  await expect(owner.getByRole("cell", { name: recipient })).toBeVisible();
  await expect(owner.getByText("Viewed", { exact: true })).toBeVisible();

  // IT Assessment: an authorization is recorded, then checks run on invented data only.
  await owner.getByRole("navigation").getByRole("link", { name: "IT Assessment" }).click();
  await owner.getByRole("link", { name: "Authorizations" }).click();
  await owner.getByLabel("Domain").first().fill("weak-dental.example");
  await owner.getByLabel("Authorized by").fill("Dana Director");
  await owner.getByLabel("Their email").fill("dana@weak-dental.example");
  await owner.getByLabel("The person named has authority").check();
  await owner.getByRole("button", { name: "Record authorization" }).click();
  await expect(owner.getByRole("cell", { name: "weak-dental.example", exact: true })).toBeVisible();
  await owner.getByRole("link", { name: "Assessments" }).click();
  await owner.getByLabel("Authorization").selectOption({ index: 1 });
  await owner.getByLabel("Template").selectOption({ label: "Email and website basics" });
  await owner.getByRole("button", { name: "Run assessment" }).click();
  await owner.getByRole("link", { name: "weak-dental.example" }).click();
  await expect(owner.getByText("SPF allows any sender")).toBeVisible({ timeout: 60_000 });
  await expect(owner.getByText("High risk").or(owner.getByText(/Score \d+/)).first()).toBeVisible();
  await expect(owner.getByText("does not test for or confirm vulnerabilities")).toBeVisible();
  await owner.screenshot({ path: "test-results/assessment.png", fullPage: true });
  void client;
});
