import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../../../app/i18n";

const post = vi.fn();
vi.mock("../../../app/api", async (orig) => ({
  ...(await orig<typeof import("../../../app/api")>()),
  useMe: () => ({ data: { tenant: { id: "t" }, permissions: ["lead.search"] } }),
  api: { GET: () => Promise.resolve({ data: { countries: [{ code: "GB", name: "United Kingdom" }], industries: [] }, response: new Response(null, { status: 200 }) }) },
  leadApi: {
    GET: () => Promise.resolve({ data: { data: [] }, response: new Response(null, { status: 200 }) }),
    POST: (...args: unknown[]) => {
      post(...args);
      return Promise.resolve({ data: { id: "s1", status: "queued" }, response: new Response(null, { status: 202 }) });
    },
  },
}));

afterEach(() => {
  cleanup();
  post.mockReset();
});

describe("company search", () => {
  it("sends a new idempotency key with each search, so a retry is never charged twice", async () => {
    const { Searches } = await import("./Searches");
    render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><Searches /></MemoryRouter></QueryClientProvider>);
    const button = await screen.findByRole("button", { name: "Find companies" });
    fireEvent.click(button);
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    fireEvent.click(button);
    await waitFor(() => expect(post).toHaveBeenCalledTimes(2));
    const key = (call: unknown[]) => (call[1] as { params: { header: Record<string, string> } }).params.header["Idempotency-Key"];
    expect(key(post.mock.calls[0] ?? [])).toBeTruthy();
    expect(key(post.mock.calls[0] ?? [])).not.toBe(key(post.mock.calls[1] ?? []));
  });
});
