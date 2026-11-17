import { TrendingUp } from "lucide-react";
import type { ModuleManifest } from "../types";
import { basePath } from "./paths";

export const manifest: ModuleManifest = {
  key: "customer_expansion",
  path: basePath,
  group: "grow",
  icon: TrendingUp,
  permission: "expansion.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
