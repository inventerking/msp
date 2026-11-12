import { fetchFile, saveBlob, unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Select, SkeletonRows, Table, type Tone } from "@msp/ui";
import { ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { assessmentApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { assessPath } from "../paths";
import { useAMutation, useAssessment, useAssessments, useAuthorizations, useTemplates } from "../api/assessments";

const riskTone: Record<string, Tone> = { low: "ok", moderate: "info", elevated: "warn", high: "danger" };
const sevTone: Record<string, Tone> = { info: "neutral", low: "info", medium: "warn", high: "danger", critical: "danger" };

export function Assessments() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const list = useAssessments();
  const auths = useAuthorizations();
  const templates = useTemplates();
  const [auth, setAuth] = useState("");
  const [template, setTemplate] = useState("");
  const start = useAMutation(() => unwrap(assessmentApi.POST("/it-assessments/assessments", { body: { authorization_id: auth, template_id: template } })));
  const active = (auths.data?.data ?? []).filter((a) => a.status === "active");
  const rows = list.data?.data ?? [];
  return (
    <div className="space-y-4">
      {me?.permissions.includes("assessment.run") ? (
        <Card className="p-4">
          <form className="grid gap-3 sm:grid-cols-[2fr_2fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); start.mutate(undefined); }}>
            <Field label={t("assess.authorization")}>{(ids) => <Select {...ids} value={auth} onChange={(e) => setAuth(e.target.value)}><option value="">{t("assess.chooseAuthorization")}</option>{active.map((a) => <option key={a.id} value={a.id}>{a.domain} ({t(`assess.tiers.${a.tier}`)})</option>)}</Select>}</Field>
            <Field label={t("assess.template")}>{(ids) => <Select {...ids} value={template} onChange={(e) => setTemplate(e.target.value)}><option value="">{t("assess.chooseTemplate")}</option>{(templates.data?.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</Select>}</Field>
            <Button type="submit" disabled={!auth || !template || start.isPending}>{t("assess.run")}</Button>
          </form>
          {active.length === 0 ? <p className="mt-2 text-xs text-ink-soft"><Link className="underline" to={assessPath("authorizations")}>{t("assess.needAuthorization")}</Link></p> : null}
          <p className="mt-2 text-xs text-ink-faint">{t("assess.fakeNote")}</p>
          {start.isError ? <div className="mt-2"><ErrorState title={errorText(t, start.error)} /></div> : null}
        </Card>
      ) : null}
      <Card>
        {list.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {list.isSuccess && rows.length === 0 ? <EmptyState icon={ShieldCheck} title={t("assess.none")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("assess.domain")}</th><th>{t("assess.status")}</th><th>{t("assess.score")}</th><th>{t("assess.when")}</th></tr></thead>
            <tbody>
              {rows.map((a) => (
                <tr key={a.id}>
                  <td><Link className="font-medium text-ink hover:underline" to={assessPath(a.id)}>{a.domain}</Link></td>
                  <td><Badge>{t(`assess.runStatus.${a.status}`)}</Badge></td>
                  <td>{a.score !== undefined ? <span className="inline-flex items-center gap-2">{a.score} {a.risk_level ? <Badge tone={riskTone[a.risk_level] ?? "neutral"}>{t(`assess.risk.${a.risk_level}`)}</Badge> : null}</span> : "—"}</td>
                  <td className="text-ink-soft">{new Date(a.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}

export function AssessmentDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const a = useAssessment(id);
  const [report, setReport] = useState<string>();
  const create = useAMutation(() => unwrap(assessmentApi.POST("/it-assessments/assessments/{id}/reports", { params: { path: { id } } })));
  const download = async (rid: string) => {
    const r = await unwrap(assessmentApi.GET("/it-assessments/reports/{id}", { params: { path: { id: rid } } }));
    if (r.status === "ready") saveBlob(await fetchFile(`/it-assessments/reports/${rid}/pdf`), "assessment.pdf");
    else setReport(rid);
  };
  if (a.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (a.isError) return <ErrorState title={errorText(t, a.error)} />;
  const d = a.data;
  return (
    <div className="space-y-4">
      <Link to={assessPath()} className="text-sm text-ink-soft hover:underline">← {t("assess.tabs.assessments")}</Link>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{d.domain}</h2>
        <Badge>{t(`assess.runStatus.${d.status}`)}</Badge>
        {d.score !== undefined ? <Badge tone={riskTone[d.risk_level ?? ""] ?? "neutral"}>{t("assess.scoreOf", { score: d.score })}</Badge> : null}
        {d.status === "completed" || d.status === "partial" ? <span className="text-xs text-ink-soft">{t("assess.coverage", { pct: Math.round(d.coverage * 100) })}</span> : null}
      </div>
      <p className="text-xs text-ink-faint" role="note">{d.notice}</p>
      {d.status === "completed" || d.status === "partial" ? (
        <div className="flex items-center gap-2">
          <Button variant="secondary" disabled={create.isPending} onClick={() => create.mutate(undefined, { onSuccess: (r) => setReport(r.id) })}>{t("assess.makeReport")}</Button>
          {report ? <Button variant="ghost" onClick={() => void download(report)}>{t("assess.downloadReport")}</Button> : null}
          {create.isError ? <span className="text-sm text-danger">{errorText(t, create.error)}</span> : null}
        </div>
      ) : null}
      <Card>
        <h3 className="px-4 pt-4 text-sm font-medium text-ink">{t("assess.findings")}</h3>
        {d.findings.length === 0 ? <p className="p-4 text-sm text-ink-soft">{t("assess.noFindings")}</p> : (
          <ul className="divide-y divide-line">
            {d.findings.map((f) => (
              <li key={f.id} className="p-4">
                <div className="flex flex-wrap items-center gap-2"><Badge tone={sevTone[f.severity] ?? "neutral"}>{t(`assess.severity.${f.severity}`)}</Badge><Badge>{t(`assess.classification.${f.classification}`)}</Badge><span className="font-medium text-ink">{f.title}</span></div>
                <p className="mt-1 text-sm text-ink-soft">{f.description}</p>
                <p className="mt-1 text-sm text-ink"><span className="font-medium">{t("assess.recommendation")}:</span> {f.recommendation}</p>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card>
        <Table>
          <thead><tr><th>{t("assess.check")}</th><th>{t("assess.result")}</th><th>{t("assess.target")}</th></tr></thead>
          <tbody>{d.checks.map((c) => <tr key={c.check_key + c.target}><td>{t(`assess.checks.${c.check_key}`, { defaultValue: c.check_key })}</td><td><Badge tone={c.status === "passed" ? "ok" : c.status === "failed" ? "danger" : "neutral"}>{t(`assess.checkStatus.${c.status}`)}</Badge></td><td className="text-ink-soft">{c.target}</td></tr>)}</tbody>
        </Table>
      </Card>
    </div>
  );
}
