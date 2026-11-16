import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { adminConsole, client, composeDir, dotenv, escapedClient, mailpit, register, stamp } from "./helpers";

let worker: ChildProcess | undefined;
test.beforeAll(() => {
  if (composeDir) return;
  const backend = resolve(import.meta.dirname, "../../../backend");
  const bin = join(tmpdir(), `msp-e2e-engage-${process.pid}`);
  execFileSync("go", ["build", "-o", bin, "./cmd/engine"], { cwd: backend, stdio: "ignore" });
  worker = spawn(bin, ["worker"], { cwd: backend, env: { ...process.env, ...dotenv() }, stdio: "ignore" });
});
test.afterAll(() => {
  worker?.kill("SIGTERM");
});

/** Waits for an email, which the worker sends on its one-minute dispatch schedule. */
async function waitForMail(to: string, pattern: RegExp): Promise<{ text: string; match: string }> {
  for (let i = 0; i < 120; i++) {
    const search = (await (await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`)).json()) as { messages?: { ID: string }[] };
    for (const m of search.messages ?? []) {
      const msg = (await (await fetch(`${mailpit}/api/v1/message/${m.ID}`)).json()) as { Text: string };
      const found = pattern.exec(msg.Text);
      if (found) return { text: msg.Text, match: found[0] };
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`no email to ${to}`);
}

test("an approved campaign emails an allowed contact who can unsubscribe, and the test site is tracked and answered by the chatbot", async ({ browser }) => {
  test.setTimeout(300_000);
  const owner = await (await browser.newContext()).newPage();
  const tenantName = `E2E Engage ${stamp}`;
  await register(owner, `e2e-engage-${stamp}@example.test`, "Erin Engage");
  await owner.getByLabel("Workspace name").fill(tenantName);
  await owner.getByRole("button", { name: "Create workspace" }).click();
  await expect(owner.getByRole("heading", { name: "Welcome, Erin Engage" })).toBeVisible();

  // An admin activates the modules, grants AI credits and allows outreach to the UK.
  const console_ = await adminConsole(browser, `e2e-admin-engage-${stamp}@example.test`);
  await console_.getByRole("navigation").getByRole("link", { name: "Tenants" }).click();
  await console_.getByRole("link", { name: tenantName }).click();
  for (const key of ["crm", "outreach", "visitor_intelligence", "chatbot"]) {
    const row = console_.locator(`tr[data-module="${key}"]`);
    await row.getByRole("button", { name: "Activate" }).click();
    await console_.getByRole("dialog").getByLabel("Reason").fill("E2E engage");
    await console_.getByRole("dialog").getByRole("button", { name: "Activate" }).click();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
  }
  const credits = console_.locator("section").filter({ has: console_.getByRole("heading", { name: "Credits" }) });
  await credits.getByLabel("Meter").selectOption("ai_credits");
  await credits.getByLabel("Amount").fill("100");
  await credits.getByLabel("Reason (kept in the audit log)").fill("E2E credits");
  await credits.getByRole("button", { name: "Grant credits" }).click();
  await expect(credits.getByRole("cell", { name: "AI credits" })).toBeVisible();
  await console_.getByRole("navigation").getByRole("link", { name: "Outreach rules" }).click();
  await console_.getByLabel("Country code").fill("GB");
  await console_.getByLabel("Rule").selectOption("opt_out_allowed");
  await console_.getByRole("button", { name: "Save rule" }).click();
  await expect(console_.getByRole("cell", { name: "GB", exact: true })).toBeVisible();
  await owner.bringToFront();
  await owner.reload();

  // A CRM contact in the UK.
  const recipient = `pat-${stamp}@example.test`;
  await owner.getByRole("navigation").getByRole("link", { name: "CRM" }).click();
  await owner.getByRole("link", { name: "Companies" }).click();
  await owner.getByLabel("Company name").fill("Engage Dental");
  await owner.getByLabel("Website").fill(`engage-dental-${stamp}.example`);
  await owner.getByRole("button", { name: "Add company" }).click();
  await owner.getByRole("link", { name: "Engage Dental" }).click();
  await owner.getByRole("textbox", { name: "Name", exact: true }).fill("Pat Practice");
  await owner.getByLabel("Email").fill(recipient);
  await owner.getByLabel("Country").selectOption({ label: "United Kingdom" });
  await owner.getByRole("button", { name: "Add person" }).click();
  await expect(owner.getByText("Pat Practice")).toBeVisible();

  // Outreach set-up: an authenticated test domain, a sender, a template and an audience.
  await owner.getByRole("navigation").getByRole("link", { name: "Outreach" }).click();
  await owner.getByRole("link", { name: "Sending set-up" }).click();
  await owner.getByRole("textbox", { name: "Domain" }).fill(`mail-${stamp}.example`);
  await owner.getByRole("button", { name: "Add domain" }).click();
  await expect(owner.getByText("Authenticated", { exact: true })).toBeVisible();
  await owner.getByRole("combobox", { name: "Domain" }).selectOption({ label: `mail-${stamp}.example` });
  await owner.getByLabel("From address").fill(`erin@mail-${stamp}.example`);
  await owner.getByLabel("From name").fill("Erin at Engage IT");
  await owner.getByRole("button", { name: "Add sender" }).click();
  await expect(owner.getByRole("cell", { name: `erin@mail-${stamp}.example` })).toBeVisible();

  await owner.getByRole("link", { name: "Templates" }).click();
  await owner.getByLabel("Template name").fill("Intro");
  await owner.getByLabel("Subject").fill("Hello {{first_name}}");
  await owner.getByLabel("Text", { exact: true }).fill("Hi {{first_name}},\nWe look after IT for dental practices.\n{{sender_name}}");
  await owner.getByRole("button", { name: "Save template" }).click();
  await expect(owner.getByRole("heading", { name: /Intro/ })).toBeVisible();

  await owner.getByRole("link", { name: "Audiences" }).click();
  await owner.getByLabel("Audience", { exact: true }).first().fill("Dental practices");
  await owner.getByRole("button", { name: "Create", exact: true }).click();
  await owner.getByRole("checkbox", { name: /Pat Practice/ }).check();
  await owner.getByRole("button", { name: "Add to audience" }).click();
  await expect(owner.getByText("1 person added")).toBeVisible();

  // A campaign: sequence, send checks, submit, approve exactly what was reviewed, start.
  await owner.getByRole("link", { name: "Campaigns" }).click();
  await owner.getByLabel("Campaign", { exact: true }).fill("Dental intro");
  await owner.getByLabel("Sender").selectOption({ index: 1 });
  await owner.getByLabel("Audience").selectOption({ index: 1 });
  await owner.getByLabel("Postal address shown in every email").fill("1 Test Street, Testville");
  await owner.getByRole("button", { name: "Create campaign" }).click();
  await expect(owner.getByRole("heading", { name: "Dental intro" })).toBeVisible();
  await owner.getByRole("button", { name: "Add step" }).click();
  await owner.getByLabel("Template").selectOption({ index: 1 });
  await owner.getByRole("button", { name: "Save sequence" }).click();
  await expect(owner.getByText("Ready to submit for approval.")).toBeVisible();
  await expect(owner.getByText("Can be emailed").locator("..").getByText("1")).toBeVisible();
  await owner.getByRole("button", { name: "Submit for approval" }).click();
  await owner.getByRole("button", { name: "Approve" }).click();
  await owner.getByRole("button", { name: "Start sending" }).click();
  await expect(owner.getByText("Sending", { exact: true }).first()).toBeVisible();

  // The worker sends it to the mail catcher, with an unsubscribe link and the postal address.
  const mail = await waitForMail(recipient, new RegExp(`${escapedClient}\\/u\\/[\\w-]+`));
  expect(mail.text).toContain("Hi Pat,");
  expect(mail.text).toContain("1 Test Street, Testville");
  const reader = await (await browser.newContext()).newPage();
  await reader.goto(mail.match);
  await expect(reader.getByText("Stop emails from Erin at Engage IT?")).toBeVisible();
  await reader.getByRole("button", { name: "Unsubscribe" }).click();
  await expect(reader.getByRole("status")).toContainText("You are unsubscribed");
  await owner.getByRole("link", { name: "Do not email" }).click();
  await expect(owner.getByRole("cell", { name: "Unsubscribed" })).toBeVisible();

  // Visitor Intelligence on the local test page, with business identification switched on.
  await owner.getByRole("navigation").getByRole("link", { name: "Visitor Intelligence" }).click();
  await owner.getByLabel("Website domain").fill(`e2e-site-${stamp}.example`);
  await owner.getByLabel("Consent mode").selectOption("opt_out");
  await owner.getByRole("button", { name: "Add", exact: true }).click();
  await expect(owner.getByRole("heading", { name: `e2e-site-${stamp}.example` })).toBeVisible();
  await owner.getByLabel("Allowed origins (comma separated)").fill(client);
  await owner.getByRole("button", { name: "Save origins" }).click();
  await owner.getByLabel("Business identification").selectOption("truncate_then_discard");
  const siteKey = /data-site="([^"]+)"/.exec(await owner.getByTestId("snippet").innerText())?.[1] ?? "";
  expect(siteKey).toMatch(/^vi_/);

  // The chatbot, with a little knowledge, allowed on the same test page.
  await owner.getByRole("navigation").getByRole("link", { name: "Chatbot" }).click();
  await owner.getByLabel("Bot name").fill("Engage helper");
  await owner.getByRole("button", { name: "Create bot" }).click();
  await expect(owner.getByRole("heading", { name: "Engage helper" })).toBeVisible();
  await owner.getByLabel("Websites it may appear on (comma separated)").fill(client);
  await owner.getByRole("button", { name: "Save changes" }).first().click();
  await owner.getByLabel("Title").fill("Services");
  await owner.getByLabel("Text", { exact: true }).fill("We provide managed backup and 24/7 monitoring for dental practices.");
  await owner.getByRole("button", { name: "Add knowledge" }).click();
  await expect(owner.getByText("1 passage")).toBeVisible();
  await owner.getByRole("button", { name: "Activate" }).click();
  await expect(owner.getByText("Active", { exact: true })).toBeVisible();
  const botKey = /data-bot="([^"]+)"/.exec(await owner.getByTestId("bot-snippet").innerText())?.[1] ?? "";
  expect(botKey).toMatch(/^cb_/);

  // A visitor browses the test page and asks the bot a question. Headless browsers name themselves
  // in their user agent and are not counted, so the visitor uses an ordinary one.
  const visitor = await (await browser.newContext({ userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0" })).newPage();
  await visitor.goto(`${client}/test-site/index.html?site=${siteKey}&mode=opt_out&bot=${botKey}`);
  await visitor.getByRole("link", { name: "Go to thank-you page" }).click();
  await expect(visitor.getByRole("heading", { name: "Thank you" })).toBeVisible();
  await visitor.getByRole("button", { name: "Chat with us" }).click();
  await expect(visitor.getByText(/automated assistant/)).toBeVisible();
  await visitor.getByLabel("Your message").fill("Do you provide managed backup for dental practices?");
  await visitor.getByRole("button", { name: "Send", exact: true }).click();
  await expect(visitor.locator('[data-role="assistant"]').last()).toContainText("managed backup");

  // The team sees the visits, the identified test company, and the conversation.
  await owner.getByRole("navigation").getByRole("link", { name: "Visitor Intelligence" }).click();
  await owner.getByRole("link", { name: `e2e-site-${stamp}.example` }).click();
  await expect(async () => {
    await owner.reload();
    await expect(owner.getByTestId("visitor-totals").getByText("Page views").locator("..").locator("dd")).toHaveText("2", { timeout: 2000 });
  }).toPass({ timeout: 30_000 });
  await expect(owner.getByText("loopback-visitor.example").first()).toBeVisible();
  await owner.getByRole("button", { name: "Send to CRM" }).click();
  await expect(owner.getByText("Added to CRM.")).toBeVisible();
  await owner.getByRole("navigation").getByRole("link", { name: "Chatbot" }).click();
  await owner.getByRole("link", { name: "Conversations" }).click();
  await owner.getByRole("link", { name: "Engage helper" }).first().click();
  await expect(owner.getByText("Do you provide managed backup for dental practices?")).toBeVisible();
});
