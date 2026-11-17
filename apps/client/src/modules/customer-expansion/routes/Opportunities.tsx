import { Badge, Card, EmptyState, ErrorState, Field, Select, SkeletonRows, Table } from "@msp/ui";
import { Target } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { errorText } from "../../../app/errors";
import { money, useOpportunities, useOpportunitySummary } from "../api/expansion";
import { expansionPath } from "../paths";

/** Accepted suggestions followed to revenue. Estimates and realised MRR are never added together. */
export function Opportunities() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<"open" | "in_deal" | "won" | "lost" | "">("");
  const opps = useOpportunities(status || undefined);
  const summary = useOpportunitySummary();
  const list = opps.data?.data ?? [];
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <h2 className="mb-2 text-sm font-medium text-ink">{t("expansion.summary")}</h2>
        {summary.isError ? <ErrorState title={errorText(t, summary.error)} /> : null}
        {summary.data && summary.data.data.length === 0 ? <p className="text-sm text-ink-soft">{t("expansion.noOpportunities")}</p> : null}
        {summary.data?.data.length ? (
          <Table>
            <thead><tr><th>{t("expansion.status")}</th><th className="text-right">{t("expansion.count")}</th><th className="text-right">{t("expansion.estimatedMrr")}</th><th className="text-right">{t("expansion.realisedMrr")}</th></tr></thead>
            <tbody>
              {summary.data.data.map((s) => (
                <tr key={`${s.status}-${s.currency ?? ""}`}>
                  <td>{t(`expansion.oppStatus.${s.status}`)}</td>
                  <td className="text-right tabular-nums">{s.count}</td>
                  <td className="text-right tabular-nums">{money(s.estimated_mrr_minor, s.currency)} <Badge tone="info">{t("intelKinds.estimated")}</Badge></td>
                  <td className="text-right tabular-nums">{money(s.realised_mrr_minor, s.currency)} <Badge tone="neutral">{t("intelKinds.actual")}</Badge></td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      <Field label={t("expansion.status")}>{(ids) => (
        <Select {...ids} value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
          <option value="">{t("expansion.allStatuses")}</option>
          {(["open", "in_deal", "won", "lost"] as const).map((s) => <option key={s} value={s}>{t(`expansion.oppStatus.${s}`)}</option>)}
        </Select>
      )}</Field>
      <Card>
        {opps.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {opps.isError ? <div className="p-4"><ErrorState title={errorText(t, opps.error)} onRetry={() => void opps.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {opps.isSuccess && list.length === 0 ? <EmptyState icon={Target} title={t("expansion.noOpportunities")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("expansion.customer")}</th><th>{t("expansion.kind")}</th><th>{t("expansion.status")}</th><th className="text-right">{t("expansion.estimatedMrr")}</th><th className="text-right">{t("expansion.realisedMrr")}</th><th>{t("expansion.deal")}</th></tr></thead>
            <tbody>
              {list.map((o) => (
                <tr key={o.id}>
                  <td><Link className="text-brand underline" to={expansionPath(`customers/${o.customer.id}`)}>{o.customer.name}</Link></td>
                  <td>{t(`expansion.kinds.${o.kind}`, { defaultValue: o.kind })}</td>
                  <td><Badge tone={o.status === "won" ? "ok" : o.status === "lost" ? "danger" : "neutral"}>{t(`expansion.oppStatus.${o.status}`)}</Badge></td>
                  <td className="text-right tabular-nums">{money(o.estimated_mrr_minor, o.currency)}</td>
                  <td className="text-right tabular-nums">{money(o.realised_mrr_minor, o.currency)}</td>
                  <td>{o.deal_id ? <Link className="text-brand underline" to={`/crm/deals/${o.deal_id}`}>{t("expansion.openDeal")}</Link> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
