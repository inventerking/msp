import { unwrap } from "@msp/api-client";
import { Badge, Button, ErrorState } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { expansionApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { money, type Recommendation, useCan, useXMutation } from "../api/expansion";
import { expansionPath } from "../paths";

/** One suggestion with its rationale and every piece of evidence. Estimates are labelled as such. */
export function RecommendationCard({ r, showCustomer = true }: { r: Recommendation; showCustomer?: boolean }) {
  const { t } = useTranslation();
  const canWrite = useCan("expansion.write");
  const [key] = useState(() => crypto.randomUUID());
  const accept = useXMutation((createDeal: boolean) =>
    unwrap(expansionApi.POST("/customer-expansion/recommendations/{id}/accept", { params: { path: { id: r.id }, header: { "Idempotency-Key": key } }, body: { create_deal: createDeal } })),
  );
  const dismiss = useXMutation(() => unwrap(expansionApi.POST("/customer-expansion/recommendations/{id}/dismiss", { params: { path: { id: r.id } } })));
  const sellable = r.kind !== "confirm_coverage";
  return (
    <article className="rounded-md border border-line p-3" aria-label={t(`expansion.kinds.${r.kind}`)}>
      <div className="mb-1 flex flex-wrap items-center gap-2">
        <Badge tone={r.kind === "confirm_coverage" ? "info" : r.kind === "renewal" ? "warn" : "ok"}>{t(`expansion.kinds.${r.kind}`)}</Badge>
        {showCustomer ? <Link className="text-sm font-medium text-brand underline" to={expansionPath(`customers/${r.customer.id}`)}>{r.customer.name}</Link> : null}
        {r.service ? <span className="text-sm text-ink">{r.service.name}</span> : null}
        <Badge tone="neutral">{t(`expansion.statuses.${r.status}`)}</Badge>
        {r.ai_worded ? <span className="text-xs text-ink-faint">{t("expansion.aiWorded")}</span> : null}
      </div>
      <p className="text-sm text-ink">{r.rationale}</p>
      {r.estimated_mrr_minor !== undefined ? (
        <p className="mt-1 text-sm"><Badge tone="info">{t("expansion.estimate")}</Badge> <span className="tabular-nums">{money(r.estimated_mrr_minor, r.currency)}</span> <span className="text-xs text-ink-soft">{t("expansion.perMonth")} · {t(`expansion.evidenceClass.${r.evidence_class}`)}</span></p>
      ) : null}
      <details className="mt-2 text-sm">
        <summary className="cursor-pointer text-ink-soft">{t("expansion.evidence", { count: r.evidence.length })}</summary>
        <ul className="mt-1 list-disc space-y-0.5 pl-5 text-ink-soft">
          {r.evidence.map((e, i) => <li key={i}>{t(`expansion.evidenceKinds.${e.kind}`, { ...flatten(e.detail) })}</li>)}
        </ul>
      </details>
      {canWrite && r.status === "open" ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {sellable ? <Button size="sm" disabled={accept.isPending} onClick={() => accept.mutate(true)}>{t("expansion.acceptWithDeal")}</Button> : null}
          <Button size="sm" variant="secondary" disabled={accept.isPending} onClick={() => accept.mutate(false)}>{sellable ? t("expansion.acceptOnly") : t("expansion.willConfirm")}</Button>
          <Button size="sm" variant="secondary" disabled={dismiss.isPending} onClick={() => dismiss.mutate(undefined)}>{t("expansion.dismiss")}</Button>
        </div>
      ) : null}
      {accept.data?.deal_skipped ? <p role="status" className="mt-2 text-xs text-ink-soft">{t(`expansion.dealSkipped.${accept.data.deal_skipped}`)}</p> : null}
      {accept.data?.deal_pending ? <p role="status" className="mt-2 text-xs text-ink-soft">{t("expansion.dealPending")}</p> : null}
      {accept.isError || dismiss.isError ? <div className="mt-2"><ErrorState title={errorText(t, accept.error ?? dismiss.error)} /></div> : null}
    </article>
  );
}

/** Evidence details are flat facts; nested values are shown as text. */
function flatten(d: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(d)) out[k] = v === null || v === undefined ? "—" : String(v);
  if (typeof d.median_mrr_minor === "number" && typeof d.currency === "string") out.median = money(d.median_mrr_minor, d.currency);
  return out;
}
