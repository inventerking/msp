import { ApiError, fetchFile, saveBlob, unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, SkeletonRows, Table } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { proposalApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { money } from "../api/proposals";

/** What a customer sees from a share link: only the frozen offer, without signing in. */
export function PublicProposal() {
  const { t } = useTranslation();
  const { token = "" } = useParams();
  const q = useQuery({ queryKey: ["public-proposal", token], queryFn: () => unwrap(proposalApi.GET("/public/proposals/{token}", { params: { path: { token } } })), retry: false });
  return (
    <main className="mx-auto max-w-3xl p-6">
      {q.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {q.isError ? <ErrorState title={q.error instanceof ApiError && q.error.code === "link_unavailable" ? t("proposals.linkGone") : errorText(t, q.error)} /> : null}
      {q.data ? (
        <div className="space-y-4">
          {q.data.superseded ? <p role="note" className="rounded-md bg-warn-soft p-3 text-sm text-warn">{t("proposals.supersededNotice")}</p> : null}
          {q.data.expired ? <p role="note" className="rounded-md bg-warn-soft p-3 text-sm text-warn">{t("proposals.expiredNotice")}</p> : null}
          <header>
            <h1 className="text-2xl font-semibold text-ink">{q.data.title}</h1>
            <p className="text-sm text-ink-soft">{t("proposals.preparedFor", { customer: q.data.customer, version: q.data.version })}{q.data.valid_until ? ` · ${t("proposals.validTo", { date: q.data.valid_until })}` : ""}</p>
          </header>
          {q.data.sections.map((s, i) => <section key={i}><h2 className="text-lg font-medium text-ink">{s.heading}</h2><p className="text-sm whitespace-pre-wrap text-ink">{s.body}</p></section>)}
          <Card>
            <Table>
              <thead><tr><th>{t("proposals.service")}</th><th>{t("proposals.quantity")}</th><th>{t("proposals.monthly")}</th><th>{t("proposals.setupFee")}</th></tr></thead>
              <tbody>{q.data.totals.lines.map((l, i) => <tr key={i}><td>{l.name}</td><td>{l.quantity}</td><td>{money(l.recurring_total_minor, q.data.totals.currency)}</td><td>{money(l.setup_fee_minor, q.data.totals.currency)}</td></tr>)}</tbody>
            </Table>
            <dl className="grid gap-2 p-4 text-sm sm:grid-cols-3">
              <div><dt className="text-ink-soft">{t("proposals.setupTotal")}</dt><dd className="font-semibold">{money(q.data.totals.setup_total_minor, q.data.totals.currency)}</dd></div>
              <div><dt className="text-ink-soft">{t("proposals.monthly")}</dt><dd className="font-semibold">{money(q.data.totals.mrr_total_minor, q.data.totals.currency)}</dd></div>
              <div><dt className="text-ink-soft">{t("proposals.tcvFor", { count: q.data.totals.term_months })}</dt><dd className="font-semibold">{money(q.data.totals.tcv_minor, q.data.totals.currency)}</dd></div>
            </dl>
            <p className="px-4 pb-4 text-xs text-ink-faint">{q.data.totals.tax_note}</p>
          </Card>
          {q.data.sla ? <section><h2 className="text-lg font-medium">{t("proposals.serviceLevels")}</h2><p className="text-sm whitespace-pre-wrap">{q.data.sla}</p></section> : null}
          {q.data.terms ? <section><h2 className="text-lg font-medium">{t("proposals.terms")}</h2><p className="text-sm whitespace-pre-wrap">{q.data.terms}</p></section> : null}
          {q.data.has_pdf ? <Button variant="secondary" onClick={() => void fetchFile(`/public/proposals/${token}/pdf`).then((b) => saveBlob(b, `${q.data.title}.pdf`))}>{t("proposals.downloadPdf")}</Button> : <Badge>{t("proposals.pdfPending")}</Badge>}
        </div>
      ) : null}
    </main>
  );
}
