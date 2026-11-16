import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCampaign, useCan, useMessages, useOMutation, usePreflight, useTemplates } from "../api/outreach";
import { statusTone } from "./Campaigns";

const editable = new Set(["draft", "pending_approval", "approved"]);

/** One campaign: its sequence, the send checks, approval, and what was sent. */
export function CampaignDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const canWrite = useCan("outreach.write");
  const canApprove = useCan("outreach.approve");
  const canSend = useCan("outreach.send");
  const c = useCampaign(id);
  const templates = useTemplates();
  const preflight = usePreflight(id, Boolean(c.data && editable.has(c.data.status)));
  const messages = useMessages(id);
  const [steps, setSteps] = useState<{ template_version_id: string; delay_minutes: number }[] | null>(null);
  const [reason, setReason] = useState("");
  const saveSteps = useOMutation(() => unwrap(outreachApi.PUT("/outreach/campaigns/{id}/steps", { params: { path: { id } }, body: { steps: steps ?? [] } })));
  const submit = useOMutation(() => unwrap(outreachApi.POST("/outreach/campaigns/{id}/submit", { params: { path: { id } } })));
  const approve = useOMutation(() => unwrap(outreachApi.POST("/outreach/campaigns/{id}/approve", { params: { path: { id } }, body: { content_hash: c.data!.content_hash } })));
  const reject = useOMutation(() => unwrap(outreachApi.POST("/outreach/campaigns/{id}/reject", { params: { path: { id } }, body: { reason } })));
  const run = useOMutation(async (action: "start" | "pause" | "resume" | "cancel") => {
    const opts = { params: { path: { id } } };
    switch (action) {
      case "start": return unwrap(outreachApi.POST("/outreach/campaigns/{id}/start", opts));
      case "pause": return unwrap(outreachApi.POST("/outreach/campaigns/{id}/pause", opts));
      case "resume": return unwrap(outreachApi.POST("/outreach/campaigns/{id}/resume", opts));
      case "cancel": return unwrap(outreachApi.POST("/outreach/campaigns/{id}/cancel", opts));
    }
  });
  if (c.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (c.isError) return <ErrorState title={errorText(t, c.error)} />;
  const camp = c.data;
  const versions = (templates.data?.data ?? []).filter((x) => x.current_version_id);
  const draftSteps = steps ?? camp.steps.map((s) => ({ template_version_id: s.template_version_id, delay_minutes: s.delay_minutes }));
  const failure = [saveSteps, submit, approve, reject, run].find((m) => m.isError)?.error;
  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{camp.name}</h2>
        <Badge tone={statusTone(camp.status)}>{t(`outreach.campaignStatus.${camp.status}`)}</Badge>
        {camp.approved ? <Badge tone="ok">{t("outreach.approvedContent")}</Badge> : null}
      </header>
      {failure ? <ErrorState title={errorText(t, failure)} /> : null}
      <Card className="p-4">
        <h3 className="mb-2 text-sm font-medium text-ink">{t("outreach.sequence")}</h3>
        {draftSteps.map((s, i) => {
          const shown = camp.steps.find((x) => x.template_version_id === s.template_version_id);
          return (
            <div key={i} className="mb-3 rounded-md border border-line p-3">
              <div className="grid gap-2 sm:grid-cols-3 sm:items-end">
                <span className="text-sm font-medium">{t("outreach.step", { n: i + 1 })}</span>
                {canWrite && camp.status === "draft" ? (
                  <>
                    <Field label={t("outreach.template")}>{(ids) => <Select {...ids} value={s.template_version_id} onChange={(e) => setSteps(draftSteps.map((x, j) => (j === i ? { ...x, template_version_id: e.target.value } : x)))}><option value="">{t("outreach.choose")}</option>{versions.map((v) => <option key={v.current_version_id} value={v.current_version_id}>{v.name} (v{v.version})</option>)}</Select>}</Field>
                    <Field label={t("outreach.delayDays")}>{(ids) => <Input {...ids} type="number" min={0} disabled={i === 0} value={Math.round(s.delay_minutes / 1440)} onChange={(e) => setSteps(draftSteps.map((x, j) => (j === i ? { ...x, delay_minutes: Number(e.target.value) * 1440 } : x)))} />}</Field>
                  </>
                ) : <span className="text-xs text-ink-soft">{t("outreach.afterDays", { count: Math.round(s.delay_minutes / 1440) })}</span>}
              </div>
              {shown ? <div className="mt-2 text-sm"><p className="font-medium">{shown.subject} {shown.ai_generated ? <Badge tone="warn">{t("outreach.aiGenerated")}</Badge> : null}</p><p className="whitespace-pre-wrap text-ink-soft">{shown.body}</p></div> : null}
            </div>
          );
        })}
        {canWrite && camp.status === "draft" ? (
          <div className="flex gap-2">
            <Button variant="secondary" disabled={draftSteps.length >= 10} onClick={() => setSteps([...draftSteps, { template_version_id: "", delay_minutes: draftSteps.length === 0 ? 0 : 3 * 1440 }])}>{t("outreach.addStep")}</Button>
            <Button disabled={!steps || steps.some((s) => !s.template_version_id) || saveSteps.isPending} onClick={() => saveSteps.mutate(undefined, { onSuccess: () => setSteps(null) })}>{t("outreach.saveSequence")}</Button>
          </div>
        ) : null}
        <p className="mt-2 text-xs text-ink-faint">{t("outreach.footerNote", { address: camp.postal_address })}</p>
      </Card>
      {preflight.data ? (
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("outreach.preflight")}</h3>
          <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-5">
            {(["total", "sendable", "suppressed", "blocked", "no_email"] as const).map((k) => <div key={k}><dt className="text-ink-soft">{t(`outreach.audienceCounts.${k}`)}</dt><dd className="font-semibold">{preflight.data.audience[k]}</dd></div>)}
          </dl>
          {preflight.data.blockers.length > 0 ? <ul className="mt-2 list-disc pl-5 text-sm text-danger">{preflight.data.blockers.map((b) => <li key={b}>{t(`outreach.blockers.${b}`, { defaultValue: b })}</li>)}</ul> : <p className="mt-2 text-sm text-ok">{t("outreach.readyToSubmit")}</p>}
          {preflight.data.warnings.includes("ai_generated_content") ? <p className="mt-1 text-sm text-warn">{t("outreach.aiWarning")}</p> : null}
        </Card>
      ) : null}
      <Card className="flex flex-wrap items-end gap-2 p-4">
        {canWrite && camp.status === "draft" ? <Button disabled={submit.isPending || (preflight.data?.blockers.length ?? 1) > 0} onClick={() => submit.mutate(undefined)}>{t("outreach.submit")}</Button> : null}
        {canApprove && camp.status === "pending_approval" ? (
          <>
            <p className="w-full text-xs text-ink-soft">{t("outreach.approveNote", { hash: camp.content_hash.slice(0, 12) })}</p>
            <Button disabled={approve.isPending} onClick={() => approve.mutate(undefined)}>{t("outreach.approve")}</Button>
            <div className="flex items-end gap-2"><Field label={t("outreach.rejectReason")}>{(ids) => <Input {...ids} value={reason} onChange={(e) => setReason(e.target.value)} />}</Field><Button variant="secondary" disabled={!reason || reject.isPending} onClick={() => reject.mutate(undefined)}>{t("outreach.reject")}</Button></div>
          </>
        ) : null}
        {canSend && camp.status === "approved" ? <Button disabled={run.isPending} onClick={() => run.mutate("start")}>{t("outreach.start")}</Button> : null}
        {canSend && camp.status === "running" ? <Button variant="secondary" onClick={() => run.mutate("pause")}>{t("outreach.pause")}</Button> : null}
        {canSend && camp.status === "paused" ? <Button onClick={() => run.mutate("resume")}>{t("outreach.resume")}</Button> : null}
        {canSend && !["completed", "cancelled"].includes(camp.status) ? <Button variant="ghost" onClick={() => run.mutate("cancel")}>{t("outreach.cancelCampaign")}</Button> : null}
      </Card>
      <Card className="p-4">
        <h3 className="mb-2 text-sm font-medium text-ink">{t("outreach.results")}</h3>
        <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-5">
          {(["sent", "delivered", "replied", "bounced", "unsubscribed"] as const).map((k) => <div key={k}><dt className="text-ink-soft">{t(`outreach.totals.${k}`)}</dt><dd className="font-semibold">{camp.totals[k]}</dd></div>)}
        </dl>
        {camp.enrollments.length > 0 ? <ul className="mt-2 text-xs text-ink-soft">{camp.enrollments.map((e, i) => <li key={i}>{t(`outreach.enrollment.${e.status}`)}{e.stop_reason ? ` · ${t(`outreach.stopReason.${e.stop_reason}`, { defaultValue: e.stop_reason })}` : ""}: {e.count}</li>)}</ul> : null}
      </Card>
      <Card>
        <Table>
          <thead><tr><th>{t("outreach.subject")}</th><th>{t("outreach.status")}</th><th>{t("outreach.sentAt")}</th></tr></thead>
          <tbody>{(messages.data?.data ?? []).map((m) => <tr key={m.id}><td>{m.subject}</td><td>{t(`outreach.messageStatus.${m.status}`, { defaultValue: m.status })}</td><td>{m.sent_at ? new Date(m.sent_at).toLocaleString() : ""}</td></tr>)}</tbody>
        </Table>
      </Card>
    </div>
  );
}
