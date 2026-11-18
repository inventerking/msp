import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../app/i18n";
import { Billing } from "./Billing";

const get = vi.fn();
const post = vi.fn();
let roles = ["billing_admin"];
vi.mock("../app/api", async (orig) => ({
  ...(await orig<typeof import("../app/api")>()),
  api: { GET: (path: string) => get(path), POST: (...a: unknown[]) => post(...a), PUT: vi.fn() },
  useAdmin: () => ({ data: { admin: { roles } } }),
}));
vi.mock("@msp/api-client", async (orig) => ({ ...(await orig<typeof import("@msp/api-client")>()), unwrap: (p: Promise<unknown>) => p }));

const queued = {
  id: "p1", tenant_id: "t1", tenant_name: "Acme MSP", plan_name: "Monthly", quantity: 1, payment_method: "card",
  quote: { total_minor: 4900, currency: "USD" }, waiting_hours: 30, over_target: true,
};

function show() {
  get.mockImplementation((path: string) => {
    switch (path) {
      case "/billing/purchases": return Promise.resolve({ data: [queued], review_target_hours: 24 });
      case "/billing/reconciliation": return Promise.resolve({ runs: [], issues: [] });
      case "/billing/dunning-policy": return Promise.resolve({ retry_days: [1, 3, 7], suspend_after_days: 14, expire_after_days: 30, bank_transfer_due_days: 14, review_target_hours: 24 });
      case "/billing/bank-instructions": return Promise.resolve({ text: "" });
      default: return Promise.resolve({ data: [] });
    }
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter><Billing /></MemoryRouter></QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  get.mockReset();
  post.mockReset();
  roles = ["billing_admin"];
});

describe("purchase review queue", () => {
  it("flags a purchase over the review target and refuses a rejection without a reason", async () => {
    show();
    await screen.findByText("Acme MSP");
    expect(screen.getByText("Over target")).toBeTruthy();
    const reject = screen.getByRole("button", { name: "Reject and refund" }) as HTMLButtonElement;
    expect(reject.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Reason for Acme MSP"), { target: { value: "Not an MSP" } });
    expect(reject.disabled).toBe(false);
    post.mockResolvedValue({});
    fireEvent.click(reject);
    await waitFor(() => expect(post).toHaveBeenCalledWith("/billing/purchases/{id}/reject", { params: { path: { id: "p1" } }, body: { reason: "Not an MSP" } }));
  });

  it("shows the queue to a platform analyst without the buttons that act", async () => {
    roles = ["platform_analyst"];
    show();
    await screen.findByText("Acme MSP");
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByText("Failed payment rules")).toBeNull();
  });
});
