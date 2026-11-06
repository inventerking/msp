import { TrendingUp } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "customer_expansion",
  path: "expansion",
  group: "grow",
  icon: TrendingUp,
  permission: "expansion.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
