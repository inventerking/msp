import { ErrorState, SkeletonRows } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Navigate, Outlet, useLocation } from "react-router";
import { useMe } from "../app/api";
import { TenantSuspended } from "./StatusPages";

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto mt-24 max-w-md px-4">{children}</div>;
}

/** Decides where a session belongs. This is presentation only: the API enforces every rule itself. */
export function RequireSession() {
  const { t } = useTranslation();
  const me = useMe();
  const location = useLocation();
  if (me.isPending) return <Centered><SkeletonRows label={t("common.loading")} /></Centered>;
  if (me.isError) return <Centered><ErrorState title={t("errors.internal_error")} onRetry={() => void me.refetch()} retryLabel={t("common.retry")} /></Centered>;
  if (!me.data) return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  if (me.data.mfa_required) return <Navigate to="/mfa" replace />;
  return <Outlet />;
}

export function RequireTenant() {
  const me = useMe();
  if (!me.data?.tenant) return <Navigate to="/workspace" replace />;
  if (me.data.tenant.status !== "active") return <TenantSuspended />;
  return <Outlet />;
}
