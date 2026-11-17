import { BarChart3 } from "lucide-react";
import type { ModuleManifest } from "../types";
import { basePath } from "./paths";

export const manifest: ModuleManifest = {
  key: "sales_intelligence",
  path: basePath,
  group: "grow",
  icon: BarChart3,
  permission: "sales_intel.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
