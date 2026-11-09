import { Search } from "lucide-react";
import { basePath } from "./paths";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "lead_generation",
  path: basePath,
  group: "prospect",
  icon: Search,
  permission: "lead.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
