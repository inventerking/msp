import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows } from "@msp/ui";
import { Lightbulb } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { competitorApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { EvidenceBadge } from "../components/Evidence";
import { useCiMutation, useOpportunities } from "../api/competitors";

const statuses = ["new", "reviewing", "pursuing", "dismissed"] as const;

export function Opportunities() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("competitor.write"));
  const opps = useOpportunities();
  const [title, setTitle] = useState("");
  const [rationale, setRationale] = useState("");
  const add = useCiMutation(() => unwrap(competitorApi.POST("/competitor-intelligence/opportunities", { body: { title, rationale } })));
  const setStatus = useCiMutation((a: { id: string; status: (typeof statuses)[number] }) => unwrap(competitorApi.PATCH("/competitor-intelligence/opportunities/{id}", { params: { path: { id: a.id } }, body: { status: a.status } })));
  const rows = opps.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canWrite ? (
        <Card className="p-4">
          <form className="grid gap-3 sm:grid-cols-[1fr_2fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); add.mutate(undefined, { onSuccess: () => { setTitle(""); setRationale(""); } }); }}>
            <Field label={t("competitors.oppTitle")}>{(ids) => <Input {...ids} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
            <Field label={t("competitors.oppRationale")}>{(ids) => <Input {...ids} value={rationale} onChange={(e) => setRationale(e.target.value)} />}</Field>
            <Button type="submit" disabled={title.length < 3 || rationale.length < 3 || add.isPending}>{t("competitors.addOpp")}</Button>
          </form>
          {add.isError ? <div className="mt-3"><ErrorState title={errorText(t, add.error)} /></div> : null}
        </Card>
      ) : null}
      <Card>
        {opps.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {opps.isError ? <div className="p-4"><ErrorState title={errorText(t, opps.error)} /></div> : null}
        {opps.isSuccess && rows.length === 0 ? <EmptyState icon={Lightbulb} title={t("competitors.noOpps")} description={t("competitors.noOppsBody")} /> : null}
        <ul className="divide-y divide-line">
          {rows.map((o) => (
            <li key={o.id} className="flex flex-wrap items-start justify-between gap-3 p-4">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2"><EvidenceBadge evidence={o.evidence_class} /><span className="font-medium text-ink">{o.title}</span></div>
                <p className="mt-1 text-sm text-ink-soft">{o.rationale}</p>
              </div>
              <Field label={t("competitors.statusLabel")}>
                {(ids) => (
                  <Select {...ids} disabled={!canWrite} value={o.status} onChange={(e) => setStatus.mutate({ id: o.id, status: e.target.value as (typeof statuses)[number] })}>
                    {statuses.map((s) => <option key={s} value={s}>{t(`competitors.oppStatus.${s}`)}</option>)}
                  </Select>
                )}
              </Field>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}
