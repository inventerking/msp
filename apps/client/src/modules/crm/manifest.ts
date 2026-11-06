import { KanbanSquare } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "crm",
  path: "crm",
  group: "sell",
  icon: KanbanSquare,
  permission: "crm.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
