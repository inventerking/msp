import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../app/i18n";
import type { MeContext } from "../app/api";
import type { ModuleManifest } from "../modules/types";
import { ModuleRoute } from "./ModuleRoute";
import { RequireSession, RequireTenant } from "./guards";

let me: MeContext | null = null;
vi.mock("../app/api", async (orig) => ({
  ...(await orig<typeof import("../app/api")>()),
  useMe: () => ({ data: me, isPending: false, isError: false }),
}));

afterEach(cleanup);

const base: MeContext = {
  user: { id: "u", email: "a@b.co", display_name: "A", mfa_enabled: false },
  mfa_required: false,
  memberships: [],
  permissions: ["crm.read"],
  modules: [{ key: "crm", status: "active" }],
  csrf_token: "c",
  tenant: { id: "t", name: "T", slug: "t", status: "active" },
};

const manifest: ModuleManifest = { key: "crm", path: "crm", group: "sell", icon: (() => null) as never, permission: "crm.read", load: async () => ({ Component: () => null }) };

function renderAt(initial = "/x") {
  const router = createMemoryRouter(
    [
      { path: "/login", element: <p>login page</p> },
      { path: "/mfa", element: <p>mfa page</p> },
      { path: "/workspace", element: <p>workspace page</p> },
      {
        element: <RequireSession />,
        children: [{ element: <RequireTenant />, children: [{ path: "/x", element: <ModuleRoute manifest={manifest} />, children: [{ index: true, element: <p>module content</p> }] }] }],
      },
    ],
    { initialEntries: [initial] },
  );
  render(<QueryClientProvider client={new QueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
}

describe("route guards", () => {
  it("sends anonymous visitors to sign-in", async () => {
    me = null;
    renderAt();
    await waitFor(() => expect(screen.getByText("login page")).toBeTruthy());
  });

  it("holds an unverified session at MFA", async () => {
    me = { ...base, mfa_required: true };
    renderAt();
    await waitFor(() => expect(screen.getByText("mfa page")).toBeTruthy());
  });

  it("asks for a workspace when none is active", async () => {
    me = { ...base, tenant: null };
    renderAt();
    await waitFor(() => expect(screen.getByText("workspace page")).toBeTruthy());
  });

  it("shows a suspended workspace page instead of content", async () => {
    me = { ...base, tenant: { id: "t", name: "T", slug: "t", status: "suspended" } };
    renderAt();
    await waitFor(() => expect(screen.getByText("This workspace is suspended")).toBeTruthy());
    expect(screen.queryByText("module content")).toBeNull();
  });

  it("shows an active module to someone with permission", async () => {
    me = base;
    renderAt();
    await waitFor(() => expect(screen.getByText("module content")).toBeTruthy());
  });

  it("explains a module that is not active", async () => {
    me = { ...base, modules: [{ key: "crm", status: "suspended" }] };
    renderAt();
    await waitFor(() => expect(screen.getByText("This module is suspended")).toBeTruthy());
    expect(screen.queryByText("module content")).toBeNull();
  });

  it("explains a pending module", async () => {
    me = { ...base, modules: [{ key: "crm", status: "pending" }] };
    renderAt();
    await waitFor(() => expect(screen.getByText("Activation is pending")).toBeTruthy());
  });

  it("denies an entitled module without the permission", async () => {
    me = { ...base, permissions: [] };
    renderAt();
    await waitFor(() => expect(screen.getByText("You do not have access to this")).toBeTruthy());
    expect(screen.queryByText("module content")).toBeNull();
  });
});
