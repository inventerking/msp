import { FileText } from "lucide-react";
import type { ModuleManifest } from "../types";
import { basePath } from "./paths";

export const manifest: ModuleManifest = {
  key: "proposals",
  path: basePath,
  group: "sell",
  icon: FileText,
  permission: "proposal.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
  publicRoutes: [
    {
      path: "/p/:token",
      load: async () => {
        const m = await import("./routes/PublicProposal");
        return { Component: m.PublicProposal };
      },
    },
  ],
};
