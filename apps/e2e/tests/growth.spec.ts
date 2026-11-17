import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, type Browser, type Page, test } from "@playwright/test";
import { adminConsole, client, composeDir, dotenv, register, stamp } from "./helpers";

let worker: ChildProcess | undefined;
test.beforeAll(() => {
  if (composeDir) return;
  const backend = resolve(import.meta.dirname, "../../../backend");
  const bin = join(tmpdir(), `msp-e2e-growth-${process.pid}`);
  execFileSync("go", ["build", "-o", bin, "./cmd/engine"], { cwd: backend, stdio: "ignore" });
  worker = spawn(bin, ["worker"], { cwd: backend, env: { ...process.env, ...dotenv() }, stdio: "ignore" });
});
test.afterAll(() => {
  worker?.kill("SIGTERM");
});

async function workspace(browser: Browser, who: string, name: string, tenantName: string): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await register(page, `e2e-${who}-${stamp}@example.test`, name);
  await page.getByLabel("Workspace name").fill(tenantName);
  await page.getByRole("button", { name: "Create workspace" }).click();
  await expect(page.getByRole("heading", { name: `Welcome, ${name}` })).toBeVisible();
  return page;
}

async function activate(console_: Page, tenantName: string, keys: string[]) {
  await console_.getByRole("navigation").getByRole("link", { name: "Tenants" }).click();
  await console_.getByRole("link", { name: tenantName }).click();
  for (const key of keys) {
    const row = console_.locator(`tr[data-module="${key}"]`);
    await row.getByRole("button", { name: "Activate" }).click();
    await console_.getByRole("dialog").getByLabel("Reason").fill("E2E growth");
    await console_.getByRole("dialog").getByRole("button", { name: "Activate" }).click();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
  }
}

test("a won deal becomes a customer whose MRR Sales Intelligence reports, a suggestion opens a deal, and another tenant sees none of it", async ({ browser }) => {
  test.setTimeout(300_000);
  const tenantA = `E2E Growth ${stamp}`;
  const tenantB = `E2E Growth Other ${stamp}`;
  const owner = await workspace(browser, "growth", "Gale Grower", tenantA);
  const other = await workspace(browser, "growth-b", "Bo Other", tenantB);
  const console_ = await adminConsole(browser, `e2e-admin-growth-${stamp}@example.test`);
  await activate(console_, tenantA, ["crm", "customer_expansion", "sales_intelligence"]);
  await activate(console_, tenantB, ["sales_intelligence"]);

  // Settings: report in pounds, and list a service the workspace sells.
  await owner.bringToFront();
  await owner.reload();
  await owner.getByRole("navigation").getByRole("link", { name: "Settings" }).click();
  await owner.getByLabel("Reporting currency").fill("GBP");
  await owner.getByRole("button", { name: "Save changes" }).click();
  await expect(owner.getByRole("status").getByText("Saved")).toBeVisible();
  await owner.getByLabel("Service", { exact: true }).fill("Managed backup");
  await owner.getByLabel("Category").selectOption({ label: "Backup and disaster recovery" });
  await owner.getByRole("button", { name: "Add service" }).click();
  await expect(owner.getByRole("cell", { name: "Managed backup" })).toBeVisible();

  // CRM: a deal worth £1,200 a month, won.
  await owner.getByRole("navigation").getByRole("link", { name: "CRM" }).click();
  await owner.getByRole("link", { name: "Companies" }).click();
  await owner.getByLabel("Company name").fill("Growth Clinic");
  await owner.getByLabel("Website").fill(`growth-clinic-${stamp}.example`);
  await owner.getByRole("button", { name: "Add company" }).click();
  await owner.getByRole("link", { name: "Pipeline" }).click();
  await owner.getByLabel("Company", { exact: true }).selectOption({ label: "Growth Clinic" });
  await owner.getByLabel("Deal", { exact: true }).fill("Support for Growth Clinic");
  await owner.getByLabel("Monthly value").fill("1200");
  await owner.getByRole("button", { name: "Add deal" }).click();
  await owner.getByRole("link", { name: "Support for Growth Clinic" }).click();
  await owner.getByRole("button", { name: "Mark won" }).click();
  await expect(owner.getByText("Won", { exact: true }).first()).toBeVisible();

  // Customer Expansion: the customer and contract arrive without re-entering anything.
  await owner.getByRole("navigation").getByRole("link", { name: "Customer Expansion" }).click();
  // The profile can appear a moment before its contract, as the two arrive by separate events.
  const customer = owner.getByRole("link", { name: "Growth Clinic" });
  const mrr = owner.getByRole("cell", { name: "£1,200.00" });
  for (let i = 0; i < 30 && !(await mrr.isVisible()); i++) {
    await owner.waitForTimeout(1000);
    await owner.reload();
  }
  await expect(mrr).toBeVisible();
  await customer.click();
  await expect(owner.getByRole("cell", { name: "Won deal" })).toBeVisible();
  const coverage = owner.getByLabel("Coverage of Managed backup");
  await expect(coverage).toHaveValue("unknown");
  await coverage.selectOption("absent");
  await owner.getByRole("button", { name: "Save coverage" }).click();
  await owner.getByRole("button", { name: "Run analysis" }).click();
  await expect(owner.getByRole("status").getByText(/Analysis done: 1 new/)).toBeVisible();
  const rec = owner.getByRole("article", { name: "Cross-sell" });
  await expect(rec.getByText(/does not have Managed backup/)).toBeVisible();
  await rec.getByText(/piece of evidence/).click();
  await expect(rec.getByText(/Your coverage records say they do not have Managed backup/)).toBeVisible();
  await rec.getByRole("button", { name: "Accept and open a deal" }).click();
  await owner.getByRole("link", { name: "Opportunities" }).click();
  await expect(owner.getByRole("link", { name: "Open deal" })).toBeVisible();

  // Sales Intelligence: MRR matches the contract, labelled actual, once metrics are computed.
  await owner.getByRole("navigation").getByRole("link", { name: "Sales Intelligence" }).click();
  await owner.getByRole("link", { name: "Revenue" }).click();
  const mrrRow = owner.getByRole("row").filter({ has: owner.getByRole("rowheader", { name: /^MRR/ }) });
  for (let i = 0; i < 40 && !(await mrrRow.getByText("£1,200").isVisible()); i++) {
    await owner.waitForTimeout(1500);
    await owner.reload();
  }
  await expect(mrrRow.getByText("£1,200")).toBeVisible();
  await expect(mrrRow.getByText("Actual")).toBeVisible();
  await expect(owner.getByText(/^Computed /)).toBeVisible();
  await owner.getByRole("link", { name: "Forecasts" }).click();
  await owner.getByRole("button", { name: "Forecast revenue" }).click();
  await expect(owner.getByText("A revenue forecast needs at least 3 complete months of contract history")).toBeVisible();

  // Tenant B: nothing of A's, and a section explains why it is empty.
  await other.bringToFront();
  await other.reload();
  await other.getByRole("navigation").getByRole("link", { name: "Sales Intelligence" }).click();
  await expect(other.getByText("The modules that feed this section are not active, and there is no history yet.").first()).toBeVisible();
  await expect(other.getByText("£1,200")).toHaveCount(0);
  await other.goto(`${client}/insights/revenue`);
  await expect(other.getByText("Growth Clinic")).toHaveCount(0);
});
