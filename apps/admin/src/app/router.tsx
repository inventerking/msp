import { createBrowserRouter } from "react-router";
import { Abuse } from "../features/Abuse";
import { Account } from "../features/Account";
import { Billing } from "../features/Billing";
import { Legal } from "../features/Legal";
import { Admins } from "../features/Admins";
import { Audit } from "../features/Audit";
import { Catalog } from "../features/Catalog";
import { Dashboard } from "../features/Dashboard";
import { Ai } from "../features/Ai";
import { Integrations } from "../features/Integrations";
import { Login } from "../features/Login";
import { Operations } from "../features/Operations";
import { OutreachPolicy } from "../features/OutreachPolicy";
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
          { path: "/integrations", Component: Integrations },
          { path: "/operations", Component: Operations },
          { path: "/ai", Component: Ai },
          { path: "/outreach-policy", Component: OutreachPolicy },
          { path: "/billing", Component: Billing },
          { path: "/legal", Component: Legal },
          { path: "/abuse", Component: Abuse },
        ],
      },
    ],
  },
]);
