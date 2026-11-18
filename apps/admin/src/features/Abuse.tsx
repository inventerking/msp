import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, PageHeader, Select, SkeletonRows } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { api, errorText } from "../app/api";

type Status = "new" | "reviewing" | "actioned" | "dismissed";

/** Abuse reports from anyone. Suspending a workspace is done on its tenant page, with a reason. */
export function Abuse() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [status, setStatus] = useState<Status | "">("new");
  const reports = useQuery({ queryKey: ["abuse", status], queryFn: () => unwrap(api.GET("/abuse-reports", { params: { query: status ? { status } : {} } })) });
  const [draft, setDraft] = useState<Record<string, { decision: string; tenant: string }>>({});
  const review = useMutation({
    mutationFn: (v: { id: string; status: Exclude<Status, "new"> }) => {
      const d = draft[v.id] ?? { decision: "", tenant: "" };
      return unwrap(api.PATCH("/abuse-reports/{id}", { params: { path: { id: v.id } }, body: { status: v.status, ...(d.decision.trim() ? { decision: d.decision.trim() } : {}), ...(d.tenant.trim() ? { target_tenant_id: d.tenant.trim() } : {}) } }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["abuse"] }),
  });
  const list = reports.data?.data ?? [];
  return (
    <>
      <PageHeader title={t("abuse.title")} description={t("abuse.subtitle")} />
      <div className="mb-3 max-w-xs"><Field label={t("abuse.status")}>{(ids) => <Select {...ids} value={status} onChange={(e) => setStatus(e.target.value as Status | "")}><option value="">{t("abuse.all")}</option>{(["new", "reviewing", "actioned", "dismissed"] as const).map((s) => <option key={s} value={s}>{t(`abuse.statuses.${s}`)}</option>)}</Select>}</Field></div>
      {reports.isPending ? <Card><SkeletonRows label={t("common.loading")} /></Card> : null}
      {reports.isError ? <ErrorState title={errorText(t, reports.error)} /> : null}
      {reports.isSuccess && list.length === 0 ? <Card><EmptyState title={t("abuse.none")} /></Card> : null}
      <div className="space-y-3">
        {list.map((r) => {
          const d = draft[r.id] ?? { decision: r.decision ?? "", tenant: r.target_tenant_id ?? "" };
          return (
            <Card key={r.id} className="p-4">
              <div className="mb-1 flex flex-wrap items-center gap-2"><Badge tone="warn">{t(`abuse.categories.${r.category}`, { defaultValue: r.category })}</Badge><Badge>{t(`abuse.statuses.${r.status}`)}</Badge><span className="text-xs text-ink-faint">{new Date(r.created_at).toLocaleString()}</span></div>
              <p className="whitespace-pre-wrap text-sm">{r.details}</p>
              {r.subject_url ? <p className="mt-1 break-all text-xs text-ink-soft">{r.subject_url}</p> : null}
              {r.reporter_email ? <p className="mt-1 text-xs text-ink-soft">{t("abuse.reporter")}: {r.reporter_email}</p> : null}
              {r.target_tenant_id ? <p className="mt-1 text-xs"><Link className="text-brand underline" to={`/tenants/${r.target_tenant_id}`}>{t("abuse.openTenant")}</Link></p> : null}
              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                <Field label={t("abuse.decision")}>{(ids) => <Input {...ids} maxLength={2000} value={d.decision} onChange={(e) => setDraft({ ...draft, [r.id]: { ...d, decision: e.target.value } })} />}</Field>
                <Field label={t("abuse.tenantId")}>{(ids) => <Input {...ids} value={d.tenant} onChange={(e) => setDraft({ ...draft, [r.id]: { ...d, tenant: e.target.value } })} />}</Field>
              </div>
              <div className="mt-2 flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" disabled={review.isPending} onClick={() => review.mutate({ id: r.id, status: "reviewing" })}>{t("abuse.markReviewing")}</Button>
                <Button size="sm" disabled={review.isPending || !d.decision.trim()} onClick={() => review.mutate({ id: r.id, status: "actioned" })}>{t("abuse.markActioned")}</Button>
                <Button size="sm" variant="secondary" disabled={review.isPending || !d.decision.trim()} onClick={() => review.mutate({ id: r.id, status: "dismissed" })}>{t("abuse.dismiss")}</Button>
              </div>
            </Card>
          );
        })}
      </div>
      {review.isError ? <div className="mt-3"><ErrorState title={errorText(t, review.error)} /></div> : null}
    </>
  );
}
