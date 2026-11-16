import { Send } from "lucide-react";
import type { ModuleManifest } from "../types";
import { basePath } from "./paths";

export const manifest: ModuleManifest = {
  key: "outreach",
  path: basePath,
  group: "engage",
  icon: Send,
  permission: "outreach.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
  publicRoutes: [
    {
      path: "/u/:token",
      load: async () => {
        const m = await import("./routes/Unsubscribe");
        return { Component: m.Unsubscribe };
      },
    },
  ],
};
