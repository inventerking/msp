import type { LucideIcon } from "lucide-react";
import type { ComponentType } from "react";

export type ModuleGroup = "prospect" | "engage" | "sell" | "grow";

/** What a feature module tells the shell. The shell knows nothing else about the module. */
export interface ModuleManifest {
  key: string;
  path: string;
  group: ModuleGroup;
  icon: LucideIcon;
  /** Permission required to open the module at all. */
  permission: string;
  load: () => Promise<{ Component: ComponentType }>;
}
