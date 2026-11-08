import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table, type Tone } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { api, errorText, hasRole, useAdmin } from "../app/api";

type Action = "activate" | "suspend" | "expire" | "revoke";

const tone: Record<string, Tone> = { active: "ok", pending: "info", suspended: "warn", expired: "warn", revoked: "danger" };

export function TenantDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const path = { params: { path: { tenantId: id } } };
  const tenant = useQuery({ queryKey: ["tenant", id], queryFn: () => unwrap(api.GET("/tenants/{tenantId}", path)) });
  const ents = useQuery({ queryKey: ["tenant", id, "entitlements"], queryFn: () => unwrap(api.GET("/tenants/{tenantId}/entitlements", path)) });
  const canSeeUsers = hasRole(me, "super_admin", "platform_support");
  const users = useQuery({ queryKey: ["tenant", id, "users"], queryFn: () => unwrap(api.GET("/tenants/{tenantId}/users", path)), enabled: canSeeUsers });
  const canManage = hasRole(me, "super_admin", "billing_admin");
  const isSuper = hasRole(me, "super_admin");
  const plans = useQuery({ queryKey: ["plans"], queryFn: () => unwrap(api.GET("/plans")), enabled: canManage });
  const modules = useQuery({ queryKey: ["modules"], queryFn: () => unwrap(api.GET("/modules")) });
  const [pendingAction, setPendingAction] = useState<{ module: string; action: Action }>();
  const [tenantAction, setTenantAction] = useState<"suspend" | "reactivate">();
  const [plan, setPlan] = useState("");
  const credits = useQuery({ queryKey: ["tenant", id, "credits"], queryFn: () => unwrap(api.GET("/tenants/{tenantId}/credits", path)) });
  const [grantAmount, setGrantAmount] = useState("");
  const [grantReason, setGrantReason] = useState("");
  const [grantKey, setGrantKey] = useState(() => crypto.randomUUID());
  const [grantError, setGrantError] = useState<string>();
  const grant = async () => {
    setGrantError(undefined);
    try {
      // The key stays the same until success, so a retried click grants once.
      await unwrap(api.POST("/tenants/{tenantId}/credits", { ...path, body: { meter_key: "ai_credits", amount: Number(grantAmount), reason: grantReason, idempotency_key: grantKey } }));
      setGrantAmount("");
      setGrantReason("");
      setGrantKey(crypto.randomUUID());
      await qc.invalidateQueries({ queryKey: ["tenant", id, "credits"] });
    } catch (e) {
      setGrantError(errorText(t, e));
    }
  };
  const [subError, setSubError] = useState<string>();
  const refresh = () => qc.invalidateQueries({ queryKey: ["tenant", id] });

  const change = useMutation({
    mutationFn: (v: { module: string; action: Action; reason: string }) =>
      unwrap(api.POST("/tenants/{tenantId}/entitlements/{moduleKey}/{action}", { params: { path: { tenantId: id, moduleKey: v.module, action: v.action } }, body: { reason: v.reason } })),
    onSuccess: async () => { setPendingAction(undefined); await refresh(); },
  });
  const changeTenant = useMutation({
    mutationFn: (v: { action: "suspend" | "reactivate"; reason: string }) =>
      unwrap(v.action === "suspend" ? api.POST("/tenants/{tenantId}/suspend", { ...path, body: { reason: v.reason } }) : api.POST("/tenants/{tenantId}/reactivate", { ...path, body: { reason: v.reason } })),
    onSuccess: async () => { setTenantAction(undefined); await refresh(); await qc.invalidateQueries({ queryKey: ["tenants"] }); },
  });
  const addSub = async () => {
    setSubError(undefined);
    const selected = plans.data?.data.find((p) => p.id === plan);
    const currency = selected?.prices.find((p) => !p.valid_to)?.currency;
    if (!selected || !currency) {
      setSubError(t("tenant.planHasNoPrice"));
      return;
    }
    try {
      await unwrap(api.POST("/tenants/{tenantId}/subscriptions", { ...path, body: { plan_id: plan, currency: currency.trim() } }));
      setPlan("");
      await refresh();
      await qc.invalidateQueries({ queryKey: ["subscriptions"] });
    } catch (e) {
      setSubError(errorText(t, e));
    }
  };
  const nameOf = (key: string) => modules.data?.data.find((m) => m.key === key)?.name ?? key;

  return (
    <>
      <PageHeader
        title={tenant.data?.name ?? t("common.loading")}
        description={tenant.data?.slug}
        actions={tenant.data && isSuper ? (
          tenant.data.status === "suspended"
            ? <Button variant="secondary" onClick={() => setTenantAction("reactivate")}>{t("tenant.reactivate")}</Button>
            : <Button variant="danger" onClick={() => setTenantAction("suspend")}>{t("tenant.suspend")}</Button>
        ) : undefined}
      />
      {tenant.isError ? <div className="mb-4"><ErrorState title={errorText(t, tenant.error)} /></div> : null}
      {tenant.data?.status === "suspended" ? <div className="mb-4"><Badge tone="warn">{t("tenants.states.suspended")}</Badge></div> : null}
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("tenant.modules")}</h2>
        <Card>
          {ents.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
          {ents.isError ? <div className="p-4"><ErrorState title={errorText(t, ents.error)} onRetry={() => void ents.refetch()} retryLabel={t("common.retry")} /></div> : null}
          {ents.data ? (
            <Table>
              <thead><tr><th>{t("tenant.module")}</th><th>{t("tenant.status")}</th>{canManage ? <th /> : null}</tr></thead>
              <tbody>
                {ents.data.data.map((e) => {
                  const action = (a: Action, label: string) => <Button key={a} variant={a === "activate" ? "secondary" : "ghost"} size="sm" onClick={() => setPendingAction({ module: e.key, action: a })}>{label}</Button>;
                  return (
                    <tr key={e.key} data-module={e.key}>
                      <td>{nameOf(e.key)}</td>
                      <td><Badge tone={tone[e.status] ?? "neutral"}>{t(`status.${e.status}`, { defaultValue: e.status })}</Badge></td>
                      {canManage ? (
                        <td className="space-x-1 text-right whitespace-nowrap">
                          {e.status !== "active" && (isSuper || e.status === "suspended" || e.status === "pending" || e.status === "expired") ? action("activate", t("tenant.actions.activate")) : null}
                          {e.status === "active" ? action("suspend", t("tenant.actions.suspend")) : null}
                          {e.status === "active" || e.status === "suspended" ? action("expire", t("tenant.actions.expire")) : null}
                          {isSuper && e.status !== "inactive" && e.status !== "revoked" && e.status !== "pending" ? action("revoke", t("tenant.actions.revoke")) : null}
                        </td>
                      ) : null}
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      {canManage ? (
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("tenant.recordSubscription")}</h2>
          <Card className="p-4">
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-64 flex-1">
                <Field label={t("tenant.plan")} hint={t("tenant.planHint")}>
                  {(ids) => <Select {...ids} value={plan} onChange={(e) => setPlan(e.target.value)}><option value="">{t("tenant.choosePlan")}</option>{plans.data?.data.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</Select>}
                </Field>
              </div>
              <Button disabled={!plan} onClick={() => void addSub()}>{t("tenant.record")}</Button>
            </div>
            {subError ? <div className="mt-3"><ErrorState title={subError} /></div> : null}
          </Card>
        </section>
      ) : null}
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("tenant.credits")}</h2>
        <Card>
          {credits.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {credits.data?.balances.length === 0 ? <EmptyState title={t("tenant.noCredits")} /> : null}
          {credits.data?.balances.length ? (
            <Table>
              <thead><tr><th>{t("tenant.meter")}</th><th>{t("tenant.amount")}</th><th /></tr></thead>
              <tbody>{credits.data.balances.map((b) => <tr key={b.meter_key}><td>{b.name}</td><td className="tabular-nums">{b.balance}</td><td className="text-xs text-ink-faint tabular-nums">{b.reserved > 0 ? `${b.reserved} reserved` : ""}</td></tr>)}</tbody>
            </Table>
          ) : null}
          {canManage ? (
            <div className="flex flex-wrap items-end gap-3 border-t border-line p-4">
              <div className="w-36"><Field label={t("tenant.amount")}>{(ids) => <Input {...ids} type="number" min={1} value={grantAmount} onChange={(e) => setGrantAmount(e.target.value)} />}</Field></div>
              <div className="min-w-64 flex-1"><Field label={t("tenant.reason")}>{(ids) => <Input {...ids} value={grantReason} onChange={(e) => setGrantReason(e.target.value)} />}</Field></div>
              <Button disabled={!(Number(grantAmount) > 0) || grantReason.trim().length < 3} onClick={() => void grant()}>{t("tenant.grant")}</Button>
              {grantError ? <div className="w-full"><ErrorState title={grantError} /></div> : null}
            </div>
          ) : null}
        </Card>
      </section>
      {canSeeUsers ? (
        <section>
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("tenant.users")}</h2>
          <Card>
            {users.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
            {users.data?.data.length === 0 ? <EmptyState title={t("tenant.noUsers")} /> : null}
            {users.data?.data.length ? (
              <Table>
                <thead><tr><th>{t("tenant.name")}</th><th>{t("tenant.email")}</th><th>{t("tenant.role")}</th></tr></thead>
                <tbody>{users.data.data.map((u) => <tr key={u.user_id}><td>{u.display_name}</td><td className="text-ink-soft">{u.email}</td><td><code className="text-xs">{u.role}</code></td></tr>)}</tbody>
              </Table>
            ) : null}
          </Card>
        </section>
      ) : null}
      <ConfirmDialog
        open={Boolean(pendingAction)}
        danger={pendingAction?.action !== "activate"}
        title={pendingAction ? t(`tenant.confirm.${pendingAction.action}`, { module: nameOf(pendingAction.module), tenant: tenant.data?.name }) : ""}
        description={pendingAction ? t(`tenant.confirmBody.${pendingAction.action}`) : ""}
        reasonLabel={t("tenant.reason")}
        confirmLabel={pendingAction ? t(`tenant.actions.${pendingAction.action}`) : ""}
        cancelLabel={t("common.cancel")}
        busy={change.isPending}
        error={change.error ? errorText(t, change.error) : undefined}
        onCancel={() => { setPendingAction(undefined); change.reset(); }}
        onConfirm={(reason) => pendingAction && change.mutate({ ...pendingAction, reason })}
      />
      <ConfirmDialog
        open={Boolean(tenantAction)}
        danger={tenantAction === "suspend"}
        title={tenantAction ? t(`tenant.tenantConfirm.${tenantAction}`, { tenant: tenant.data?.name }) : ""}
        reasonLabel={t("tenant.reason")}
        confirmLabel={tenantAction ? t(`tenant.${tenantAction}`) : ""}
        cancelLabel={t("common.cancel")}
        busy={changeTenant.isPending}
        error={changeTenant.error ? errorText(t, changeTenant.error) : undefined}
        onCancel={() => setTenantAction(undefined)}
        onConfirm={(reason) => tenantAction && changeTenant.mutate({ action: tenantAction, reason })}
      />
    </>
  );
}
