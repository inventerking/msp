import { cleanup, render, screen } from "@testing-library/react";
import { createMemoryRouter, Outlet, Route, RouterProvider, Routes } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import { moduleChildren } from "./moduleRoutes";

afterEach(cleanup);

function Module() {
  return (
    <Routes>
      <Route index element={<p>module home</p>} />
      <Route path="items/:id" element={<p>module item</p>} />
    </Routes>
  );
}

describe("module routes", () => {
  for (const [url, text] of [["/m", "module home"], ["/m/items/1", "module item"]] as const) {
    it(`serves ${url}`, async () => {
      const router = createMemoryRouter([{ path: "/m", element: <Outlet />, children: moduleChildren(async () => ({ Component: Module })) }], { initialEntries: [url] });
      render(<RouterProvider router={router} />);
      expect(await screen.findByText(text)).toBeTruthy();
    });
  }
});
