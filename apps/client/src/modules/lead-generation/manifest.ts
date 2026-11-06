import { Search } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "lead_generation",
  path: "leads",
  group: "prospect",
  icon: Search,
  permission: "lead.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
