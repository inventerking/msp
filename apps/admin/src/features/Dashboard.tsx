import { unwrap } from "@msp/api-client";
import { Card, PageHeader, Skeleton } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { api, hasRole, useAdmin } from "../app/api";

function Stat({ label, value, to, loading }: { label: string; value: number; to: string; loading: boolean }) {
  return (
    <Link to={to} className="block rounded-lg border border-line bg-surface p-4 hover:border-line-strong">
      <p className="text-xs text-ink-faint">{label}</p>
      {loading ? <Skeleton className="mt-1 h-7 w-12" /> : <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>}
    </Link>
  );
}

export function Dashboard() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const canView = hasRole(me, "super_admin", "platform_support", "billing_admin", "platform_analyst");
  const tenants = useQuery({ queryKey: ["tenants", "first"], queryFn: () => unwrap(api.GET("/tenants", { params: { query: { limit: 100 } } })), enabled: canView });
  const subs = useQuery({ queryKey: ["subscriptions"], queryFn: () => unwrap(api.GET("/subscriptions")), enabled: canView });
  const pending = subs.data?.data.filter((s) => s.status === "pending_activation").length ?? 0;
  const suspended = tenants.data?.data.filter((x) => x.status === "suspended").length ?? 0;
  return (
    <>
      <PageHeader title={t("dashboard.title")} description={t("dashboard.subtitle", { name: me?.admin.display_name })} />
      {canView ? (
        <div className="grid gap-3 sm:grid-cols-3">
          <Stat label={t("dashboard.awaiting")} value={pending} to="/subscriptions" loading={subs.isPending} />
          <Stat label={t("dashboard.tenants")} value={tenants.data?.data.length ?? 0} to="/tenants" loading={tenants.isPending} />
          <Stat label={t("dashboard.suspended")} value={suspended} to="/tenants" loading={tenants.isPending} />
        </div>
      ) : <Card className="p-4 text-sm text-ink-soft">{t("dashboard.limited")}</Card>}
    </>
  );
}
