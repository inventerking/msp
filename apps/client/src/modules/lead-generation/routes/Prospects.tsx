import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { Search } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { leadPath } from "../paths";
import { Link, useSearchParams } from "react-router";
import { leadApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { QualityBadge, ScoreBadge, StatusBadge } from "../components/Labels";
import { type ProspectStatus, useLeadMutation, useLists, useProspects } from "../api/leads";

const statuses: ProspectStatus[] = ["new", "qualified", "disqualified", "converted"];

export function Prospects() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("lead.write"));
  const [status, setStatus] = useState<ProspectStatus | "">("");
  const [minScore, setMinScore] = useState("");
  const [params] = useSearchParams();
  const [listId, setListId] = useState(params.get("list") ?? "");
  const [q, setQ] = useState("");
  const filter = { ...(status ? { status } : {}), ...(minScore ? { min_score: Number(minScore) } : {}), ...(listId ? { list_id: listId } : {}), ...(q.trim() ? { q: q.trim() } : {}) };
  const prospects = useProspects(filter);
  const lists = useLists();
  const [domain, setDomain] = useState("");
  const add = useLeadMutation((d: string) => unwrap(leadApi.POST("/lead-generation/prospects", { body: { domain: d } })));
  const rows = prospects.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canWrite ? (
        <Card className="p-4">
          <form
            className="flex flex-wrap items-end gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (domain.trim()) add.mutate(domain.trim(), { onSuccess: () => setDomain("") });
            }}
          >
            <div className="min-w-64 flex-1">
              <Field label={t("leads.addByDomain")} hint={t("leads.addByDomainHint")} error={add.isError ? errorText(t, add.error) : undefined}>
                {(ids) => <Input {...ids} value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" />}
              </Field>
            </div>
            <Button type="submit" disabled={add.isPending}>{t("leads.add")}</Button>
          </form>
        </Card>
      ) : null}
      <div className="grid gap-3 sm:grid-cols-4">
        <Field label={t("leads.filterSearch")}>{(ids) => <Input {...ids} value={q} onChange={(e) => setQ(e.target.value)} />}</Field>
        <Field label={t("leads.filterStatus")}>
          {(ids) => (
            <Select {...ids} value={status} onChange={(e) => setStatus(e.target.value as ProspectStatus | "")}>
              <option value="">{t("leads.any")}</option>
              {statuses.map((s) => <option key={s} value={s}>{t(`leads.status.${s}`)}</option>)}
            </Select>
          )}
        </Field>
        <Field label={t("leads.filterMinScore")}>{(ids) => <Input {...ids} type="number" min={0} max={100} value={minScore} onChange={(e) => setMinScore(e.target.value)} />}</Field>
        <Field label={t("leads.filterList")}>
          {(ids) => (
            <Select {...ids} value={listId} onChange={(e) => setListId(e.target.value)}>
              <option value="">{t("leads.any")}</option>
              {(lists.data?.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
            </Select>
          )}
        </Field>
      </div>
      <Card>
        {prospects.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {prospects.isError ? <div className="p-4"><ErrorState title={errorText(t, prospects.error)} onRetry={() => void prospects.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {prospects.isSuccess && rows.length === 0 ? <EmptyState icon={Search} title={t("leads.noProspects")} description={t("leads.noProspectsBody")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("leads.company")}</th><th>{t("leads.score")}</th><th>{t("leads.dataQuality")}</th><th>{t("leads.statusLabel")}</th><th>{t("leads.source")}</th></tr></thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id}>
                  <td>
                    <Link to={leadPath(`prospects/${p.id}`)} className="font-medium text-ink hover:underline">{p.display_name}</Link>
                    {p.domain ? <div className="text-xs text-ink-faint">{p.domain}</div> : null}
                  </td>
                  <td><ScoreBadge score={p.score} coverage={p.coverage} /></td>
                  <td><QualityBadge quality={p.data_quality} /></td>
                  <td><StatusBadge status={p.status} /></td>
                  <td className="text-ink-soft">{t(`leads.sources.${p.source_key}`, { defaultValue: p.source_key })}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
