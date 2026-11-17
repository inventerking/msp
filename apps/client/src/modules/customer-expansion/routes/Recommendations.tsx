import { Card, EmptyState, ErrorState, Field, Select, SkeletonRows } from "@msp/ui";
import { Lightbulb } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { errorText } from "../../../app/errors";
import { type Recommendation, useRecommendations } from "../api/expansion";
import { RecommendationCard } from "../components/RecommendationCard";

const statuses = ["open", "accepted", "converted", "dismissed", "expired"] as const;

/** Every suggestion across customers. Each one shows the evidence behind it. */
export function Recommendations() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<Recommendation["status"]>("open");
  const recs = useRecommendations({ status });
  const list = recs.data?.data ?? [];
  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-soft">{t("expansion.recommendationsHelp")}</p>
      <Field label={t("expansion.status")}>{(ids) => (
        <Select {...ids} value={status} onChange={(e) => setStatus(e.target.value as Recommendation["status"])}>
          {statuses.map((s) => <option key={s} value={s}>{t(`expansion.statuses.${s}`)}</option>)}
        </Select>
      )}</Field>
      <Card className="p-4">
        {recs.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {recs.isError ? <ErrorState title={errorText(t, recs.error)} onRetry={() => void recs.refetch()} retryLabel={t("common.retry")} /> : null}
        {recs.isSuccess && list.length === 0 ? <EmptyState icon={Lightbulb} title={t("expansion.noRecommendations")} description={t("expansion.runAnalysis")} /> : null}
        <div className="space-y-3">{list.map((r) => <RecommendationCard key={r.id} r={r} />)}</div>
      </Card>
    </div>
  );
}
