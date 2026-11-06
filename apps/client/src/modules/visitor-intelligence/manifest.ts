import { Globe } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "visitor_intelligence",
  path: "visitors",
  group: "engage",
  icon: Globe,
  permission: "visitor.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
