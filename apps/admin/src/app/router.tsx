import { createBrowserRouter } from "react-router";
import { Account } from "../features/Account";
import { Admins } from "../features/Admins";
import { Audit } from "../features/Audit";
import { Catalog } from "../features/Catalog";
import { Dashboard } from "../features/Dashboard";
import { Login } from "../features/Login";
import { Subscriptions } from "../features/Subscriptions";
import { TenantDetail } from "../features/TenantDetail";
import { Tenants } from "../features/Tenants";
import { RequireAdmin, Shell } from "../shell/Shell";

export const router = createBrowserRouter([
  { path: "/login", Component: Login },
  {
    Component: RequireAdmin,
    children: [
      {
        Component: Shell,
        children: [
          { index: true, path: "/", Component: Dashboard },
          { path: "/tenants", Component: Tenants },
          { path: "/tenants/:id", Component: TenantDetail },
          { path: "/catalog", Component: Catalog },
          { path: "/subscriptions", Component: Subscriptions },
          { path: "/audit", Component: Audit },
          { path: "/admins", Component: Admins },
          { path: "/account", Component: Account },
        ],
      },
    ],
  },
]);
