import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { api, errorText, hasRole, useAdmin } from "../app/api";

export function Subscriptions() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const subs = useQuery({ queryKey: ["subscriptions"], queryFn: () => unwrap(api.GET("/subscriptions")) });
  const canManage = hasRole(me, "super_admin", "billing_admin");
  const [activating, setActivating] = useState<string>();
  const activate = useMutation({
    mutationFn: (v: { id: string; reason: string }) => unwrap(api.POST("/subscriptions/{id}/activate", { params: { path: { id: v.id } }, body: { reason: v.reason } })),
    onSuccess: async () => { setActivating(undefined); await qc.invalidateQueries(); },
  });
  return (
    <>
      <PageHeader title={t("subscriptions.title")} description={t("subscriptions.subtitle")} />
      <Card>
        {subs.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {subs.isError ? <div className="p-4"><ErrorState title={errorText(t, subs.error)} onRetry={() => void subs.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {subs.data?.data.length === 0 ? <EmptyState title={t("subscriptions.empty")} /> : null}
        {subs.data?.data.length ? (
          <Table>
            <thead><tr><th>{t("subscriptions.plan")}</th><th>{t("subscriptions.tenant")}</th><th>{t("subscriptions.status")}</th><th>{t("subscriptions.created")}</th>{canManage ? <th /> : null}</tr></thead>
            <tbody>
              {subs.data.data.map((s) => (
                <tr key={s.id}>
                  <td>{s.plan_name}</td>
                  <td><Link className="text-brand hover:underline" to={`/tenants/${s.tenant_id}`}>{t("subscriptions.openTenant")}</Link></td>
                  <td><Badge tone={s.status === "active" ? "ok" : s.status === "pending_activation" ? "info" : "neutral"}>{t(`subscriptions.states.${s.status}`, { defaultValue: s.status })}</Badge></td>
                  <td className="text-ink-soft">{new Date(s.created_at).toLocaleDateString()}</td>
                  {canManage ? <td className="text-right">{s.status === "pending_activation" ? <Button size="sm" onClick={() => setActivating(s.id)}>{t("subscriptions.activate")}</Button> : null}</td> : null}
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      <ConfirmDialog
        open={Boolean(activating)} title={t("subscriptions.confirmTitle")} description={t("subscriptions.confirmBody")} reasonLabel={t("tenant.reason")}
        confirmLabel={t("subscriptions.activate")} cancelLabel={t("common.cancel")} busy={activate.isPending} error={activate.error ? errorText(t, activate.error) : undefined}
        onCancel={() => setActivating(undefined)} onConfirm={(reason) => activating && activate.mutate({ id: activating, reason })}
      />
    </>
  );
}
