import { ShieldCheck } from "lucide-react";
import type { ModuleManifest } from "../types";

export const manifest: ModuleManifest = {
  key: "it_assessment",
  path: "assessments",
  group: "sell",
  icon: ShieldCheck,
  permission: "assessment.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
