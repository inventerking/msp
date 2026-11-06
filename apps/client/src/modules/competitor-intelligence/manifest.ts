import { Binoculars } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "competitor_intelligence",
  path: "competitors",
  group: "prospect",
  icon: Binoculars,
  permission: "competitor.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
