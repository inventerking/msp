import type { RouteObject } from "react-router";
import type { ModuleManifest } from "../modules/types";

/**
 * Child routes for a module. The index route serves the module's home, and the splat route lets the
 * module route its own pages below its path. A splat alone does not match the bare path.
 */
export function moduleChildren(load: ModuleManifest["load"]): RouteObject[] {
  return [
    { index: true, lazy: load },
    { path: "*", lazy: load },
  ];
}
