import { Send } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "outreach",
  path: "outreach",
  group: "engage",
  icon: Send,
  permission: "outreach.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
