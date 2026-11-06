import { unwrap } from "@msp/api-client";
import { Badge, Card, EmptyState, ErrorState, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";
import { errorText } from "../app/errors";
import { statusTone } from "../app/moduleStatus";
import { NoPermission } from "../shell/StatusPages";

export function Subscription() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const allowed = Boolean(me?.permissions.includes("billing.read"));
  const products = useQuery({ queryKey: tenantKey(me, "products"), queryFn: () => unwrap(api.GET("/catalog/products")), enabled: allowed });
  const subs = useQuery({ queryKey: tenantKey(me, "subscriptions"), queryFn: () => unwrap(api.GET("/subscriptions")), enabled: allowed });
  if (!allowed) return <NoPermission />;
  return (
    <>
      <PageHeader title={t("subscription.title")} description={t("subscription.subtitle")} />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("subscription.modules")}</h2>
        <Card>
          <Table>
            <tbody>
              {me?.modules.map((m) => <tr key={m.key}><td>{t(`modules.${m.key}.name`)}</td><td className="text-right"><Badge tone={statusTone(m.status)}>{t(`status.${m.status}`)}</Badge></td></tr>)}
            </tbody>
          </Table>
        </Card>
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("subscription.subscriptions")}</h2>
        <Card>
          {subs.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {subs.isError ? <div className="p-4"><ErrorState title={errorText(t, subs.error)} onRetry={() => void subs.refetch()} retryLabel={t("common.retry")} /></div> : null}
          {subs.data?.data.length === 0 ? <EmptyState title={t("subscription.noSubscriptions")} /> : null}
          {subs.data?.data.length ? (
            <Table>
              <thead><tr><th>{t("subscription.plan")}</th><th>{t("subscription.status")}</th><th>{t("subscription.started")}</th></tr></thead>
              <tbody>{subs.data.data.map((s) => <tr key={s.id}><td>{s.plan_name}</td><td><code className="text-xs">{s.status}</code></td><td className="text-ink-soft">{new Date(s.created_at).toLocaleDateString()}</td></tr>)}</tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      <section>
        <h2 className="mb-1 text-sm font-medium text-ink-soft">{t("subscription.plans")}</h2>
        <p className="mb-2 text-xs text-ink-faint">{t("subscription.requestNote")}</p>
        {products.isPending ? <Card><SkeletonRows label={t("common.loading")} /></Card> : null}
        {products.isError ? <ErrorState title={errorText(t, products.error)} onRetry={() => void products.refetch()} retryLabel={t("common.retry")} /> : null}
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {products.data?.data.filter((p) => p.kind !== "module").map((p) => (
            <li key={p.id}>
              <Card className="h-full p-4">
                <div className="flex items-center justify-between gap-2"><p className="text-sm font-medium">{p.name}</p><Badge>{t(`subscription.kinds.${p.kind}`)}</Badge></div>
                <p className="mt-2 text-xs text-ink-faint">{t("subscription.includes")}</p>
                <ul className="mt-1 space-y-0.5 text-xs text-ink-soft">{p.modules.map((m) => <li key={m}>{t(`modules.${m}.name`, { defaultValue: m })}</li>)}</ul>
              </Card>
            </li>
          ))}
        </ul>
      </section>
    </>
  );
}
