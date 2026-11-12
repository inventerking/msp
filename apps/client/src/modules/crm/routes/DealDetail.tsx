import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, SkeletonRows } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { crmApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { Timeline } from "../components/Timeline";
import { crmPath } from "../paths";
import { money, toMinor, useCrmMutation, useDeal } from "../api/crm";

export function DealDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const deal = useDeal(id);
  const [mrr, setMrr] = useState("");
  const [reason, setReason] = useState("");
  const [task, setTask] = useState("");
  const d = deal.data;
  const version = d?.row_version ?? 0;
  const setValue = useCrmMutation(() => unwrap(crmApi.PATCH("/crm/deals/{id}", { params: { path: { id } }, body: { row_version: version, expected_mrr_minor: toMinor(mrr, d?.currency ?? "GBP") ?? null } })));
  const win = useCrmMutation(() => unwrap(crmApi.POST("/crm/deals/{id}/win", { params: { path: { id } }, body: { row_version: version } })));
  const lose = useCrmMutation(() => unwrap(crmApi.POST("/crm/deals/{id}/lose", { params: { path: { id } }, body: { row_version: version, loss_reason: reason } })));
  const reopen = useCrmMutation(() => unwrap(crmApi.POST("/crm/deals/{id}/reopen", { params: { path: { id } } })));
  const addTask = useCrmMutation(() => unwrap(crmApi.POST("/crm/tasks", { body: { title: task, deal_id: id, ...(me ? { assignee_user_id: me.user.id } : {}) } })));
  if (deal.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (deal.isError || !d) return <ErrorState title={errorText(t, deal.error)} />;
  const err = setValue.error ?? win.error ?? lose.error ?? reopen.error;
  return (
    <div className="space-y-4">
      <Link to={crmPath()} className="text-sm text-ink-soft hover:underline">← {t("crm.tabs.pipeline")}</Link>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{d.title}</h2>
        <Badge tone={d.status === "won" ? "ok" : d.status === "lost" ? "danger" : "info"}>{d.status === "open" ? d.stage_name : t(`crm.status.${d.status}`)}</Badge>
        <Link className="text-sm text-ink-soft hover:underline" to={crmPath(`companies/${d.crm_company_id}`)}>{d.company_name}</Link>
      </div>
      {err ? <ErrorState title={errorText(t, err)} /> : null}
      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="p-4">
          <dl className="space-y-1 text-sm">
            <div className="flex justify-between"><dt className="text-ink-soft">{t("crm.monthlyValue")}</dt><dd>{money(d.expected_mrr_minor, d.currency)}</dd></div>
            <div className="flex justify-between"><dt className="text-ink-soft">{t("crm.contractValue")}</dt><dd>{money(d.contract_value_minor, d.currency)}</dd></div>
            <div className="flex justify-between"><dt className="text-ink-soft">{t("crm.probability")}</dt><dd>{d.probability}%</dd></div>
            {d.loss_reason ? <div className="flex justify-between"><dt className="text-ink-soft">{t("crm.lossReason")}</dt><dd>{d.loss_reason}</dd></div> : null}
          </dl>
          {can("crm.write") && d.status === "open" ? (
            <form className="mt-3 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); setValue.mutate(undefined, { onSuccess: () => setMrr("") }); }}>
              <div className="flex-1"><Field label={t("crm.setMonthlyValue", { currency: d.currency })}>{(ids) => <Input {...ids} inputMode="decimal" value={mrr} onChange={(e) => setMrr(e.target.value)} />}</Field></div>
              <Button type="submit" size="sm" variant="secondary" disabled={setValue.isPending}>{t("common.save")}</Button>
            </form>
          ) : null}
        </Card>
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("crm.close")}</h3>
          {d.status === "open" && can("crm.close_deal") ? (
            <div className="space-y-3">
              <Button disabled={win.isPending} onClick={() => win.mutate(undefined)}>{t("crm.markWon")}</Button>
              <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); lose.mutate(undefined); }}>
                <div className="flex-1"><Field label={t("crm.lossReason")}>{(ids) => <Input {...ids} value={reason} onChange={(e) => setReason(e.target.value)} />}</Field></div>
                <Button type="submit" variant="secondary" disabled={reason.trim().length < 2 || lose.isPending}>{t("crm.markLost")}</Button>
              </form>
            </div>
          ) : null}
          {d.status !== "open" && can("crm.configure") ? <Button variant="secondary" disabled={reopen.isPending} onClick={() => reopen.mutate(undefined)}>{t("crm.reopen")}</Button> : null}
          <h3 className="mt-4 mb-1 text-sm font-medium text-ink">{t("crm.history")}</h3>
          <ul className="space-y-1 text-xs text-ink-soft">
            {d.history.map((h, i) => <li key={i}>{new Date(h.created_at).toLocaleString()}: {h.to_stage_name}{h.reason ? ` (${h.reason})` : ""}</li>)}
          </ul>
        </Card>
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("crm.addTask")}</h3>
          {can("crm.write") ? (
            <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); addTask.mutate(undefined, { onSuccess: () => setTask("") }); }}>
              <div className="flex-1"><Field label={t("crm.taskTitle")}>{(ids) => <Input {...ids} value={task} onChange={(e) => setTask(e.target.value)} />}</Field></div>
              <Button type="submit" size="sm" disabled={!task.trim() || addTask.isPending}>{t("crm.add")}</Button>
            </form>
          ) : null}
          {addTask.isSuccess ? <p role="status" className="mt-2 text-xs text-ok">{t("crm.taskAdded")}</p> : null}
        </Card>
      </div>
      <Timeline subjectType="deal" subjectId={id} />
    </div>
  );
}
