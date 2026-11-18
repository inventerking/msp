import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../app/i18n";
import { LegalGate } from "./LegalGate";

const get = vi.fn();
const post = vi.fn();
vi.mock("../app/api", async (orig) => ({
  ...(await orig<typeof import("../app/api")>()),
  api: { GET: (...a: unknown[]) => get(...a), POST: (...a: unknown[]) => post(...a) },
  useMe: () => ({ data: { tenant: { id: "t" } } }),
}));
vi.mock("@msp/api-client", async (orig) => ({ ...(await orig<typeof import("@msp/api-client")>()), unwrap: (p: Promise<unknown>) => p }));

afterEach(() => {
  cleanup();
  get.mockReset();
  post.mockReset();
});

const terms = { id: "d1", kind: "terms", title: "Terms of service", version: 3, body: "Counsel's text", published_at: "2026-11-18T00:00:00Z" };

function show() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><LegalGate /></QueryClientProvider>);
}

describe("LegalGate", () => {
  it("shows nothing when no document is due", async () => {
    get.mockResolvedValue({ data: [] });
    show();
    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("records acceptance of the exact versions shown, only after the person confirms reading them", async () => {
    get.mockResolvedValueOnce({ data: [terms] }).mockResolvedValue({ data: [] });
    post.mockResolvedValue(undefined);
    show();
    const accept = await screen.findByRole("button", { name: "Accept and continue" });
    expect(screen.getByRole("dialog").textContent).toContain("Version 3");
    expect((accept as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByLabelText("I have read these documents"));
    fireEvent.click(accept);
    await waitFor(() => expect(post).toHaveBeenCalledWith("/me/legal/accept", { body: { document_ids: ["d1"] } }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
