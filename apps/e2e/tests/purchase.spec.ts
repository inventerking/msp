import { expect, test } from "@playwright/test";
import { adminConsole, client, register, stamp } from "./helpers";

// Uses the test payment provider, which exists only in local, test and testing environments.
test("a customer buys a plan by card, an admin approves it, and the module turns on", async ({ browser }) => {
  test.setTimeout(240_000);
  const tenant = `E2E Buyer ${stamp}`;
  const product = `E2E CRM Product ${stamp}`;
  const plan = `E2E Monthly ${stamp}`;

  // The customer's workspace, with nothing active yet.
  const owner = await (await browser.newContext()).newPage();
  await register(owner, `e2e-buyer-${stamp}@example.test`, "Bea Buyer");
  await owner.getByLabel("Workspace name").fill(tenant);
  await owner.getByRole("button", { name: "Create workspace" }).click();
  await expect(owner.getByRole("heading", { name: "Welcome, Bea Buyer" })).toBeVisible();
  await expect(owner.getByRole("navigation").getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  // The operator puts a product on sale: draft, publish, plan, price.
  const console_ = await adminConsole(browser, `e2e-admin-buy-${stamp}@example.test`);
  await console_.getByRole("navigation").getByRole("link", { name: "Catalog" }).click();
  const productForm = console_.locator("form").filter({ has: console_.getByRole("button", { name: "Create draft" }) });
  await productForm.getByLabel("Key").fill(`e2e_crm_${stamp}`);
  await productForm.getByLabel("Name").fill(product);
  await productForm.getByLabel("Kind").selectOption("module");
  await productForm.getByLabel("CRM", { exact: true }).check();
  await productForm.getByRole("button", { name: "Create draft" }).click();
  await console_.getByRole("row", { name: new RegExp(product) }).getByRole("button", { name: "Publish" }).click();
  await expect(console_.getByRole("row", { name: new RegExp(product) }).getByText(/published/)).toBeVisible();

  const planForm = console_.locator("form").filter({ has: console_.getByRole("button", { name: "Create plan" }) });
  await planForm.getByLabel("Published product").selectOption({ label: `${product} v1` });
  await planForm.getByLabel("Key").fill(`e2e_monthly_${stamp}`);
  await planForm.getByLabel("Name").fill(plan);
  await planForm.getByLabel("Seats per unit").fill("5");
  await planForm.getByRole("button", { name: "Create plan" }).click();
  await console_.getByRole("row", { name: new RegExp(plan) }).getByRole("button", { name: "Set price" }).click();
  await console_.getByLabel("Currency").fill("USD");
  await console_.getByLabel("Amount in minor units").fill("4900");
  await console_.getByRole("button", { name: "Save price" }).click();
  await expect(console_.getByRole("row", { name: new RegExp(plan) }).getByText("USD 4900")).toBeVisible();

  // The customer buys it by card and pays on the test checkout page.
  await owner.bringToFront();
  await owner.getByRole("navigation").getByRole("link", { name: "Subscription" }).click();
  await owner.getByLabel("Plan", { exact: true }).selectOption({ label: `${product} · ${plan} · $49.00 a month` });
  await owner.getByLabel("Payment method").selectOption("card");
  await owner.getByRole("button", { name: "Pay by card" }).click();
  await expect(owner.getByRole("heading", { name: "Test checkout" })).toBeVisible();
  await owner.getByRole("button", { name: "Pay", exact: true }).click();
  await expect(owner.getByText("Paid", { exact: true })).toBeVisible();
  await owner.getByRole("link", { name: "Back to your workspace" }).click();
  await expect(owner).toHaveURL(new RegExp(`^${client}`));
  await owner.goto(`${client}/subscription`);
  await expect(owner.getByRole("cell", { name: "Paid, under review" })).toBeVisible();
  await expect(owner.getByRole("navigation").getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  // The operator approves it from the review queue.
  await console_.bringToFront();
  await console_.getByRole("navigation").getByRole("link", { name: "Billing" }).click();
  const row = console_.getByRole("row", { name: new RegExp(tenant) });
  await row.getByRole("button", { name: "Approve" }).click();
  await expect(row).toHaveCount(0);

  // The module is on, and the purchase and paid invoice show.
  await owner.bringToFront();
  await owner.reload();
  await expect(owner.getByRole("navigation").getByRole("link", { name: "CRM", exact: true })).toBeVisible();
  await expect(owner.getByRole("cell", { name: "Approved" })).toBeVisible();
  await expect(owner.getByRole("cell", { name: "Paid", exact: true })).toBeVisible();
});
