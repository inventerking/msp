import { Badge, type Tone } from "@msp/ui";
import { useTranslation } from "react-i18next";

/** A score, with how much of it rests on real data. A score is never shown without its coverage. */
export function ScoreBadge({ score, coverage }: { score?: number | undefined; coverage?: number | undefined }) {
  const { t } = useTranslation();
  if (score === undefined) return <Badge>{t("leads.notScored")}</Badge>;
  const tone: Tone = score >= 75 ? "ok" : score >= 50 ? "info" : "neutral";
  return (
    <span className="inline-flex items-center gap-1.5">
      <Badge tone={tone}>{score}</Badge>
      {coverage !== undefined ? <span className="text-xs text-ink-faint">{t("leads.coverage", { pct: Math.round(coverage * 100) })}</span> : null}
    </span>
  );
}

const qualityTone: Record<string, Tone> = { high: "ok", medium: "info", low: "warn", unknown: "neutral" };

export function QualityBadge({ quality }: { quality: string }) {
  const { t } = useTranslation();
  return <Badge tone={qualityTone[quality] ?? "neutral"}>{t(`leads.quality.${quality}`, { defaultValue: quality })}</Badge>;
}

const statusTone: Record<string, Tone> = { qualified: "ok", disqualified: "danger", converted: "info", completed: "ok", failed: "danger", partial: "warn", cancelled: "neutral", running: "info", queued: "neutral", no_data: "warn", succeeded: "ok" };

export function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  return <Badge tone={statusTone[status] ?? "neutral"}>{t(`leads.status.${status}`, { defaultValue: status })}</Badge>;
}

/** Where a fact came from and what its licence allows. */
export function LicenceBadge({ licence }: { licence: string }) {
  const { t } = useTranslation();
  return <Badge tone={licence === "licensed_tenant" ? "warn" : "neutral"}>{t(`leads.licence.${licence}`, { defaultValue: licence })}</Badge>;
}
