import { fetchFile, saveBlob, unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { FileText } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ciPath } from "../paths";
import { Link, useNavigate, useParams } from "react-router";
import { competitorApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { EvidenceBadge, StateBadge } from "../components/Evidence";
import { useCiMutation, useCompetitors, useReport, useReports } from "../api/competitors";

export function Reports() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const canCreate = Boolean(me?.permissions.includes("competitor.research"));
  const reports = useReports();
  const competitors = useCompetitors();
  const [picked, setPicked] = useState<string[]>([]);
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<"competitor" | "market" | "comparison">("market");
  const create = useCiMutation(() => unwrap(competitorApi.POST("/competitor-intelligence/reports", { body: { kind, competitor_ids: picked, ...(title.trim() ? { title: title.trim() } : {}) } })));
  const rows = reports.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canCreate ? (
        <Card className="p-4">
          <h2 className="text-sm font-medium text-ink">{t("competitors.newReport")}</h2>
          <p className="mb-3 text-sm text-ink-soft">{t("competitors.reportBody")}</p>
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (r) => void navigate(ciPath(`reports/${r.id}`)) }); }}>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t("competitors.reportTitle")}>{(ids) => <Input {...ids} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
              <Field label={t("competitors.reportKind")}>
                {(ids) => (
                  <Select {...ids} value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
                    {(["market", "competitor", "comparison"] as const).map((k) => <option key={k} value={k}>{t(`competitors.reportKinds.${k}`)}</option>)}
                  </Select>
                )}
              </Field>
            </div>
            <fieldset>
              <legend className="mb-1 text-sm font-medium text-ink">{t("competitors.include")}</legend>
              <div className="flex flex-wrap gap-3">
                {(competitors.data?.data ?? []).map((c) => (
                  <label key={c.id} className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={picked.includes(c.id)} onChange={(e) => setPicked((p) => (e.target.checked ? [...p, c.id] : p.filter((x) => x !== c.id)))} />
                    {c.display_name}
                  </label>
                ))}
              </div>
            </fieldset>
            {create.isError ? <ErrorState title={errorText(t, create.error)} /> : null}
            <Button type="submit" disabled={picked.length === 0 || create.isPending}>{t("competitors.generate")}</Button>
          </form>
        </Card>
      ) : null}
      <Card>
        {reports.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {reports.isError ? <div className="p-4"><ErrorState title={errorText(t, reports.error)} /></div> : null}
        {reports.isSuccess && rows.length === 0 ? <EmptyState icon={FileText} title={t("competitors.noReports")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("competitors.reportTitle")}</th><th>{t("competitors.statusLabel")}</th><th>{t("competitors.created")}</th></tr></thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id}><td><Link className="font-medium text-ink hover:underline" to={ciPath(`reports/${r.id}`)}>{r.title}</Link></td><td><StateBadge state={r.status} /></td><td className="text-ink-soft">{new Date(r.created_at).toLocaleString()}</td></tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}

export function ReportView() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const report = useReport(id);
  const [pdfError, setPdfError] = useState<unknown>();
  if (report.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (report.isError) return <ErrorState title={errorText(t, report.error)} onRetry={() => void report.refetch()} retryLabel={t("common.retry")} />;
  const r = report.data;
  return (
    <div className="space-y-4">
      <Link to={ciPath("reports")} className="text-sm text-ink-soft hover:underline">← {t("competitors.tabs.reports")}</Link>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-ink">{r.title}</h2>
        <div className="flex items-center gap-2">
          <StateBadge state={r.status} />
          {r.has_pdf ? <Button size="sm" variant="secondary" onClick={() => void fetchFile(`/competitor-intelligence/reports/${r.id}/pdf`).then((b) => saveBlob(b, `${r.title}.pdf`), setPdfError)}>{t("competitors.downloadPdf")}</Button> : null}
        </div>
      </div>
      {pdfError ? <ErrorState title={errorText(t, pdfError)} /> : null}
      {r.status === "failed" ? <ErrorState title={t("competitors.reportFailed")} /> : null}
      {r.status === "queued" || r.status === "generating" ? <p role="status" className="text-sm text-ink-soft">{t("competitors.generating")}</p> : null}
      {r.summary ? (
        <Card className="p-4">
          <div className="mb-2 flex items-center gap-2"><h3 className="text-sm font-medium text-ink">{t("competitors.summary")}</h3>{r.summary_label ? <EvidenceBadge evidence={r.summary_label} /> : null}</div>
          <p className="text-sm whitespace-pre-line text-ink">{r.summary}</p>
        </Card>
      ) : null}
      {r.opportunities?.length ? (
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("competitors.tabs.opportunities")}</h3>
          <ul className="space-y-2">{r.opportunities.map((o) => <li key={o.id}><div className="flex items-center gap-2"><EvidenceBadge evidence={o.evidence_class} /><span className="font-medium">{o.title}</span></div><p className="text-sm text-ink-soft">{o.rationale}</p></li>)}</ul>
        </Card>
      ) : null}
      {r.facts?.length ? (
        <Card>
          <h3 className="px-4 pt-4 text-sm font-medium text-ink">{t("competitors.factsUsed")}</h3>
          <Table>
            <thead><tr><th>{t("competitors.competitor")}</th><th>{t("competitors.attribute")}</th><th>{t("competitors.value")}</th><th>{t("competitors.evidenceLabel")}</th></tr></thead>
            <tbody>{r.facts.map((f, i) => <tr key={i}><td>{f.competitor}</td><td>{t(`competitors.attributes.${f.attribute_type}`, { defaultValue: f.attribute_type })}</td><td>{f.value}</td><td><EvidenceBadge evidence={f.evidence_class} /></td></tr>)}</tbody>
          </Table>
        </Card>
      ) : null}
      {r.notice ? <p className="text-xs text-ink-faint" role="note">{r.notice}</p> : null}
    </div>
  );
}
