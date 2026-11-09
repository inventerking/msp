import { createBrowserRouter } from "react-router";
import { modules } from "../modules";
import { moduleChildren } from "./moduleRoutes";
import { ModuleRoute } from "../shell/ModuleRoute";
import { NotFound } from "../shell/StatusPages";
import { RequireSession, RequireTenant } from "../shell/guards";
import { Shell } from "../shell/Shell";
import { ForgotPassword, Login, Mfa, Register, ResetPassword, VerifyEmail } from "../platform/auth";
import { Dashboard } from "../platform/Dashboard";
import { Profile } from "../platform/Profile";
import { Settings } from "../platform/Settings";
import { Subscription } from "../platform/Subscription";
import { Team } from "../platform/Team";
import { AcceptInvite, Workspace } from "../platform/Workspace";

export const router = createBrowserRouter([
  { path: "/login", Component: Login },
  { path: "/register", Component: Register },
  { path: "/verify-email", Component: VerifyEmail },
  { path: "/forgot-password", Component: ForgotPassword },
  { path: "/reset-password", Component: ResetPassword },
  { path: "/mfa", Component: Mfa },
  { path: "/invite/:token", Component: AcceptInvite },
  {
    Component: RequireSession,
    children: [
      { path: "/workspace", Component: Workspace },
      {
        Component: RequireTenant,
        children: [
          {
            Component: Shell,
            children: [
              { index: true, path: "/", Component: Dashboard },
              { path: "/team", Component: Team },
              { path: "/settings", Component: Settings },
              { path: "/subscription", Component: Subscription },
              { path: "/profile", Component: Profile },
              ...modules.map((m) => ({
                path: `/${m.path}`,
                element: <ModuleRoute manifest={m} />,
                children: moduleChildren(m.load),
              })),
              { path: "*", Component: NotFound },
            ],
          },
        ],
      },
    ],
  },
]);
