import { FileText } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "proposals",
  path: "proposals",
  group: "sell",
  icon: FileText,
  permission: "proposal.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
