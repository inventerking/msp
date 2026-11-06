import { Outlet } from "react-router";
import { useMe } from "../app/api";
import type { ModuleManifest } from "../modules/types";
import { ModuleStatusPage, NoPermission } from "./StatusPages";

/** Shows the module, or explains why not. A backend 403 still takes precedence over this check. */
export function ModuleRoute({ manifest }: { manifest: ModuleManifest }) {
  const { data: me } = useMe();
  const status = me?.modules.find((m) => m.key === manifest.key)?.status ?? "inactive";
  if (status !== "active") return <ModuleStatusPage moduleKey={manifest.key} status={status} />;
  if (!me?.permissions.includes(manifest.permission)) return <NoPermission />;
  return <Outlet />;
}
