import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, SkeletonRows, Table } from "@msp/ui";
import { Search } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { leadApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { LicenceBadge, StatusBadge } from "../components/Labels";
import { useLeadMutation, useReference, useSearchResults, useSearches } from "../api/leads";
import { CodePicker } from "./Icp";

function Results({ id }: { id: string }) {
  const { t } = useTranslation();
  const results = useSearchResults(id);
  const [picked, setPicked] = useState<string[]>([]);
  const promote = useLeadMutation((body: { company_ids?: string[]; all?: boolean }) => unwrap(leadApi.POST("/lead-generation/searches/{id}/results/promote", { params: { path: { id } }, body })));
  const rows = results.data?.data ?? [];
  if (results.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (results.isError) return <ErrorState title={errorText(t, results.error)} />;
  if (rows.length === 0) return <EmptyState title={t("leads.noResults")} />;
  return (
    <>
      <div className="flex flex-wrap items-center gap-2 p-3">
        <Button size="sm" disabled={promote.isPending || picked.length === 0} onClick={() => promote.mutate({ company_ids: picked }, { onSuccess: () => setPicked([]) })}>{t("leads.saveSelected", { count: picked.length })}</Button>
        <Button size="sm" variant="secondary" disabled={promote.isPending} onClick={() => promote.mutate({ all: true })}>{t("leads.saveAll")}</Button>
        {promote.isSuccess ? <span role="status" className="text-sm text-ok">{t("leads.saved", { created: promote.data.created, existing: promote.data.existing })}</span> : null}
        {promote.isError ? <span className="text-sm text-danger">{errorText(t, promote.error)}</span> : null}
      </div>
      <Table>
        <thead><tr><th><span className="sr-only">{t("leads.select")}</span></th><th>{t("leads.company")}</th><th>{t("leads.country")}</th><th>{t("leads.industry")}</th><th>{t("leads.employees")}</th><th>{t("leads.licenceLabel")}</th></tr></thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.company_id}>
              <td>
                {r.prospect_id ? <span className="text-xs text-ok">{t("leads.alreadySaved")}</span> : (
                  <input type="checkbox" aria-label={t("leads.selectCompany", { name: r.display_name })} checked={picked.includes(r.company_id)}
                    onChange={(e) => setPicked((p) => (e.target.checked ? [...p, r.company_id] : p.filter((x) => x !== r.company_id)))} />
                )}
              </td>
              <td><span className="font-medium text-ink">{r.display_name}</span>{r.domain ? <div className="text-xs text-ink-faint">{r.domain}</div> : null}</td>
              <td>{r.country ?? "—"}</td>
              <td>{r.industry ? t(`industries.${r.industry}`, { defaultValue: r.industry }) : "—"}</td>
              <td>{r.employees ?? "—"}</td>
              <td><LicenceBadge licence={r.licence_class} /></td>
            </tr>
          ))}
        </tbody>
      </Table>
    </>
  );
}

export function Searches() {
  const { t } = useTranslation();
  const ref = useReference();
  const searches = useSearches();
  const [countries, setCountries] = useState<string[]>([]);
  const [industries, setIndustries] = useState<string[]>([]);
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [limit, setLimit] = useState("25");
  const [open, setOpen] = useState<string>();
  const start = useLeadMutation(() =>
    unwrap(leadApi.POST("/lead-generation/searches", {
      // A fresh key per click: a retried request returns this search instead of charging again.
      params: { header: { "Idempotency-Key": crypto.randomUUID() } },
      body: { criteria: { ...(countries.length ? { countries } : {}), ...(industries.length ? { industries } : {}), ...(min ? { employee_min: Number(min) } : {}), ...(max ? { employee_max: Number(max) } : {}) }, max_results: Number(limit) || 25 },
    })),
  );
  const list = searches.data?.data ?? [];
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); start.mutate(undefined, { onSuccess: (s) => setOpen(s.id) }); }}>
          <div className="grid gap-4 sm:grid-cols-2">
            <CodePicker label={t("leads.countries")} options={ref.data?.countries ?? []} value={countries} onChange={setCountries} />
            <CodePicker label={t("leads.industries")} options={(ref.data?.industries ?? []).map((i) => ({ code: i.code, name: t(`industries.${i.code}`, { defaultValue: i.name }) }))} value={industries} onChange={setIndustries} />
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label={t("leads.employeeMin")}>{(ids) => <Input {...ids} type="number" min={1} value={min} onChange={(e) => setMin(e.target.value)} />}</Field>
            <Field label={t("leads.employeeMax")}>{(ids) => <Input {...ids} type="number" min={1} value={max} onChange={(e) => setMax(e.target.value)} />}</Field>
            <Field label={t("leads.maxResults")} hint={t("leads.creditsHint")}>{(ids) => <Input {...ids} type="number" min={1} max={200} value={limit} onChange={(e) => setLimit(e.target.value)} />}</Field>
          </div>
          {start.isError ? <ErrorState title={errorText(t, start.error)} /> : null}
          <Button type="submit" disabled={start.isPending}>{t("leads.runSearch")}</Button>
        </form>
      </Card>
      <Card>
        <h2 className="px-4 pt-4 text-sm font-medium text-ink">{t("leads.history")}</h2>
        {searches.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {searches.isError ? <div className="p-4"><ErrorState title={errorText(t, searches.error)} /></div> : null}
        {searches.isSuccess && list.length === 0 ? <EmptyState icon={Search} title={t("leads.noSearches")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("leads.started")}</th><th>{t("leads.statusLabel")}</th><th>{t("leads.resultsCount")}</th><th>{t("leads.credits")}</th><th /></tr></thead>
            <tbody>
              {list.map((s) => (
                <tr key={s.id}>
                  <td className="whitespace-nowrap">{new Date(s.created_at).toLocaleString()}</td>
                  <td><StatusBadge status={s.status} /></td>
                  <td>{s.result_count}</td>
                  <td className="text-ink-soft">{t("leads.creditsUsed", { used: s.credits_used, reserved: s.credits_reserved })}</td>
                  <td>{s.status === "completed" || s.status === "partial" ? <Button size="sm" variant="ghost" aria-expanded={open === s.id} onClick={() => setOpen(open === s.id ? undefined : s.id)}>{open === s.id ? t("leads.hideResults") : t("leads.showResults")}</Button> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      {open && list.find((s) => s.id === open && (s.status === "completed" || s.status === "partial")) ? <Card><Results id={open} /></Card> : null}
    </div>
  );
}
