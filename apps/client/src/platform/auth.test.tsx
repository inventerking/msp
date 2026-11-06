import { ApiError } from "@msp/api-client";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../app/i18n";
import { VerifyEmail } from "./auth";

const post = vi.fn();
vi.mock("../app/api", async (orig) => ({ ...(await orig<typeof import("../app/api")>()), api: { POST: () => post() } }));
vi.mock("@msp/api-client", async (orig) => {
  const real = await orig<typeof import("@msp/api-client")>();
  return { ...real, unwrap: (p: Promise<unknown>) => p };
});

afterEach(() => {
  cleanup();
  post.mockReset();
});

function show() {
  render(<RouterProvider router={createMemoryRouter([{ path: "/verify-email", element: <VerifyEmail /> }, { path: "/login", element: <p>login</p> }], { initialEntries: ["/verify-email?token=abc"] })} />);
}

describe("VerifyEmail", () => {
  it("confirms a good link", async () => {
    post.mockResolvedValue(undefined);
    show();
    await waitFor(() => expect(screen.getByText("Email confirmed")).toBeTruthy());
  });

  it("calls a rejected token a bad link", async () => {
    post.mockRejectedValue(new ApiError(422, "token_invalid"));
    show();
    await waitFor(() => expect(screen.getByText("That link did not work")).toBeTruthy());
  });

  it("does not blame the link when the request was only rate limited", async () => {
    post.mockRejectedValue(new ApiError(429, "rate_limited"));
    show();
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Too many attempts"));
    expect(screen.queryByText("That link did not work")).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });
});
