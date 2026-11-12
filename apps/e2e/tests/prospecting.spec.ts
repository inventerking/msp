import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { adminConsole, composeDir, dotenv, register, stamp } from "./helpers";

// Background jobs need a worker. Locally it is started here; a testing server runs its own.
let worker: ChildProcess | undefined;
// The binary is built first, so the worker is ready before the first job is queued.
test.beforeAll(() => {
  if (composeDir) return;
  const backend = resolve(import.meta.dirname, "../../../backend");
  const bin = join(tmpdir(), `msp-e2e-engine-${process.pid}`);
  execFileSync("go", ["build", "-o", bin, "./cmd/engine"], { cwd: backend, stdio: "ignore" });
  worker = spawn(bin, ["worker"], { cwd: backend, env: { ...process.env, ...dotenv() }, stdio: "ignore" });
});
test.afterAll(() => {
  worker?.kill("SIGTERM");
});

test("a workspace finds, scores and saves prospects, and researches a competitor with labelled evidence", async ({ browser }) => {
  test.setTimeout(240_000);
  const customer = await (await browser.newContext()).newPage();
  const tenantName = `E2E Prospecting ${stamp}`;
  await register(customer, `e2e-leads-${stamp}@example.test`, "Lee Prospector");
  await customer.getByLabel("Workspace name").fill(tenantName);
  await customer.getByRole("button", { name: "Create workspace" }).click();
  await expect(customer.getByRole("heading", { name: "Welcome, Lee Prospector" })).toBeVisible();

  // An admin activates both prospecting modules and grants credits.
  const console_ = await adminConsole(browser, `e2e-admin-leads-${stamp}@example.test`);
  await console_.getByRole("navigation").getByRole("link", { name: "Tenants" }).click();
  await console_.getByRole("link", { name: tenantName }).click();
  for (const key of ["lead_generation", "competitor_intelligence"]) {
    const row = console_.locator(`tr[data-module="${key}"]`);
    await row.getByRole("button", { name: "Activate" }).click();
    await console_.getByRole("dialog").getByLabel("Reason").fill("E2E prospecting");
    await console_.getByRole("dialog").getByRole("button", { name: "Activate" }).click();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
  }
  const credits = console_.locator("section").filter({ has: console_.getByRole("heading", { name: "Credits" }) });
  for (const meter of ["lead_credits", "ai_credits"]) {
    await credits.getByLabel("Meter").selectOption(meter);
    await credits.getByLabel("Amount").fill("100");
    await credits.getByLabel("Reason (kept in the audit log)").fill("E2E credits");
    await credits.getByRole("button", { name: "Grant credits" }).click();
    await expect(credits.getByRole("cell", { name: meter === "lead_credits" ? "Lead credits" : "AI credits" })).toBeVisible();
  }

  // The customer sets an ideal customer profile. Their tab is in front again, as polling pauses in hidden tabs.
  await customer.bringToFront();
  await customer.reload();
  await customer.getByRole("navigation").getByRole("link", { name: "Lead Generation" }).click();
  await customer.getByRole("link", { name: "Ideal customer" }).click();
  await customer.getByLabel("Industries").selectOption("legal_services");
  await customer.getByLabel("Countries").selectOption("GB");
  await customer.getByLabel("Employees from").fill("20");
  await customer.getByLabel("Employees up to").fill("200");
  await customer.getByRole("button", { name: "Save changes" }).click();
  await expect(customer.getByText("Profile saved")).toBeVisible();

  // A search runs in the background against sample data, then its results are saved.
  await customer.getByRole("link", { name: "Find companies" }).click();
  await customer.getByLabel("Countries").selectOption("GB");
  await customer.getByRole("button", { name: "Find companies" }).click();
  // The new search's results open by themselves once the background job finishes.
  await expect(customer.getByRole("cell", { name: /^Northwind Legal/ })).toBeVisible({ timeout: 60_000 });
  await customer.getByRole("button", { name: "Save all" }).click();
  await expect(customer.getByText(/saved, 0 already saved/)).toBeVisible();

  // Prospects are scored, and a score always shows how much of it is known.
  await customer.getByRole("link", { name: "Prospects" }).click();
  await customer.getByRole("link", { name: "Northwind Legal" }).click();
  await expect(customer.getByRole("heading", { name: "Northwind Legal" })).toBeVisible();
  await expect(customer.getByText("100% known")).toBeVisible();
  await expect(customer.getByText("Unknown factors mean there is no data")).toBeVisible();
  await customer.getByRole("button", { name: "Explain" }).click();
  await expect(customer.getByText("AI generated")).toBeVisible({ timeout: 60_000 });
  await customer.getByRole("button", { name: "Convert to CRM" }).click();
  // Conversion uses the customer registry, which works even without the CRM module (D-02).
  await expect(customer.getByText("Converted", { exact: true }).first()).toBeVisible();

  // A competitor is researched: facts copied from its page are labelled Observed, with their source.
  await customer.getByRole("navigation").getByRole("link", { name: "Competitor Intelligence" }).click();
  await customer.getByRole("button", { name: "Suggest competitors" }).click();
  const suggestion = customer.getByRole("listitem").filter({ hasText: "Brightbyte IT Services" });
  await suggestion.getByRole("button", { name: "Track" }).click();
  await customer.getByRole("link", { name: "Brightbyte IT Services" }).click();
  await customer.getByRole("button", { name: "Research now" }).click();
  await expect(customer.getByText("pricing first seen", { exact: false })).toBeVisible({ timeout: 60_000 });
  await customer.reload();
  const price = customer.getByRole("listitem").filter({ hasText: "From £" }).first();
  await expect(price.getByText("Observed")).toBeVisible();
  await expect(customer.getByText("https://brightbyte-it-services.example/", { exact: false }).first()).toBeVisible();

  // An AI report is labelled as an estimate everywhere it makes a claim.
  await customer.getByRole("link", { name: "Reports" }).click();
  await customer.getByLabel("Brightbyte IT Services").check();
  await customer.getByRole("button", { name: "Generate report" }).click();
  await expect(customer.getByRole("heading", { name: "Summary" })).toBeVisible({ timeout: 60_000 });
  await expect(customer.getByText("AI estimate").first()).toBeVisible();
  await expect(customer.getByText("AI estimates are generated and may be wrong", { exact: false })).toBeVisible();
  await customer.screenshot({ path: "test-results/competitor-report.png", fullPage: true });
});
