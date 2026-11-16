import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../../../app/i18n";

let done = false;
const post = vi.fn();
vi.mock("../../../app/api", async (orig) => ({
  ...(await orig<typeof import("../../../app/api")>()),
  outreachApi: {
    GET: () => Promise.resolve({ data: { sender: "Sam Sender", done }, response: new Response(null, { status: 200 }) }),
    POST: (...args: unknown[]) => {
      post(...args);
      done = true;
      return Promise.resolve({ data: undefined, response: new Response(null, { status: 204 }) });
    },
  },
}));

afterEach(() => {
  cleanup();
  post.mockReset();
  done = false;
});

describe("unsubscribe page", () => {
  it("changes nothing until the button is pressed, so link scanners cannot opt anyone out", async () => {
    const { Unsubscribe } = await import("./Unsubscribe");
    const router = createMemoryRouter([{ path: "/u/:token", element: <Unsubscribe /> }], { initialEntries: ["/u/tok123"] });
    render(<QueryClientProvider client={new QueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
    await screen.findByText("Stop emails from Sam Sender?");
    expect(post).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Unsubscribe" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("You are unsubscribed"));
    expect(post).toHaveBeenCalledWith("/public/outreach/unsubscribe/{token}", { params: { path: { token: "tok123" } } });
  });
});
