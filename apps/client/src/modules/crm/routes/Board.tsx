import { ApiError, unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { crmApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { crmPath } from "../paths";
import { money, toMinor, useBoard, useCompanies, useCrmMutation, usePipelines } from "../api/crm";

function NewDeal({ pipeline }: { pipeline: string }) {
  const { t } = useTranslation();
  const companies = useCompanies("");
  const [company, setCompany] = useState("");
  const [title, setTitle] = useState("");
  const [currency, setCurrency] = useState("GBP");
  const [mrr, setMrr] = useState("");
  const create = useCrmMutation(() => {
    const m = toMinor(mrr, currency);
    return unwrap(crmApi.POST("/crm/deals", { body: { crm_company_id: company, title, currency, pipeline_id: pipeline, ...(m !== undefined ? { expected_mrr_minor: m } : {}) } }));
  });
  return (
    <Card className="mb-4 p-4">
      <form className="grid gap-3 sm:grid-cols-[2fr_2fr_1fr_1fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => { setTitle(""); setMrr(""); } }); }}>
        <Field label={t("crm.company")}>
          {(ids) => (
            <Select {...ids} value={company} onChange={(e) => setCompany(e.target.value)}>
              <option value="">{t("crm.chooseCompany")}</option>
              {(companies.data?.data ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </Select>
          )}
        </Field>
        <Field label={t("crm.dealTitle")}>{(ids) => <Input {...ids} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
        <Field label={t("crm.currency")}>{(ids) => <Input {...ids} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />}</Field>
        <Field label={t("crm.monthlyValue")}>{(ids) => <Input {...ids} inputMode="decimal" value={mrr} onChange={(e) => setMrr(e.target.value)} />}</Field>
        <Button type="submit" disabled={!company || !title.trim() || create.isPending}>{t("crm.addDeal")}</Button>
      </form>
      {create.isError ? <div className="mt-2"><ErrorState title={errorText(t, create.error)} /></div> : null}
    </Card>
  );
}

/** The pipeline as columns. Each card can be moved with a select, which works from the keyboard. */
export function Board() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("crm.write"));
  const pipelines = usePipelines();
  const [chosen, setChosen] = useState<string>();
  const pipeline = chosen ?? pipelines.data?.data[0]?.id;
  const board = useBoard(pipeline);
  const [conflict, setConflict] = useState(false);
  const move = useCrmMutation((a: { id: string; stage: string; version: number }) => unwrap(crmApi.POST("/crm/deals/{id}/move", { params: { path: { id: a.id } }, body: { stage_id: a.stage, row_version: a.version } })));
  if (pipelines.isPending || board.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (pipelines.isError || board.isError) return <ErrorState title={errorText(t, pipelines.error ?? board.error)} />;
  const stages = board.data?.stages ?? [];
  const openStages = stages.filter((s) => s.stage.kind === "open");
  return (
    <div>
      {(pipelines.data?.data.length ?? 0) > 1 ? (
        <div className="mb-3 max-w-xs">
          <Field label={t("crm.pipeline")}>
            {(ids) => <Select {...ids} value={pipeline} onChange={(e) => setChosen(e.target.value)}>{pipelines.data?.data.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</Select>}
          </Field>
        </div>
      ) : null}
      {canWrite && pipeline ? <NewDeal pipeline={pipeline} /> : null}
      {conflict ? <div className="mb-3"><ErrorState title={t("errors.stale_version")} /></div> : null}
      {move.isError && !conflict ? <div className="mb-3"><ErrorState title={errorText(t, move.error)} /></div> : null}
      <div className="flex gap-3 overflow-x-auto pb-2">
        {stages.map((col) => (
          <section key={col.stage.id} aria-label={col.stage.name} className="w-64 shrink-0 rounded-lg bg-sunken p-2">
            <h2 className="flex items-center justify-between px-1 pb-2 text-sm font-medium text-ink">
              <span>{col.stage.name}</span>
              <Badge>{col.deals.length}</Badge>
            </h2>
            {col.totals.map((tot) => <p key={tot.currency} className="px-1 pb-2 text-xs text-ink-soft">{t("crm.monthlyTotal", { amount: money(tot.mrr_minor, tot.currency) })}</p>)}
            <ul className="space-y-2">
              {col.deals.map((d) => (
                <li key={d.id} className="rounded-md border border-line bg-surface p-2 text-sm">
                  <Link to={crmPath(`deals/${d.id}`)} className="font-medium text-ink hover:underline">{d.title}</Link>
                  <div className="text-xs text-ink-soft">{d.company_name}</div>
                  <div className="mt-1 text-xs text-ink-faint">{money(d.expected_mrr_minor, d.currency)} {t("crm.perMonth")}</div>
                  {canWrite && d.status === "open" ? (
                    <Select aria-label={t("crm.moveDeal", { title: d.title })} className="mt-2 h-8 text-xs" value={d.stage_id}
                      onChange={(e) => { setConflict(false); move.mutate({ id: d.id, stage: e.target.value, version: d.row_version }, { onError: (err) => setConflict(err instanceof ApiError && err.code === "stale_version") }); }}>
                      {openStages.map((s) => <option key={s.stage.id} value={s.stage.id}>{s.stage.name}</option>)}
                    </Select>
                  ) : <Badge tone={d.status === "won" ? "ok" : "danger"}>{t(`crm.status.${d.status}`)}</Badge>}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  );
}
