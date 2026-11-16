import { MessageCircle } from "lucide-react";
import type { ModuleManifest } from "../types";
import { basePath } from "./paths";

export const manifest: ModuleManifest = {
  key: "chatbot",
  path: basePath,
  group: "engage",
  icon: MessageCircle,
  permission: "chatbot.read",
  load: async () => {
    const m = await import("./routes/Overview");
    return { Component: m.Overview };
  },
};
