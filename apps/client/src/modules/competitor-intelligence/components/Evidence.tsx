import { Badge, type Tone } from "@msp/ui";
import { useTranslation } from "react-i18next";

const tone: Record<string, Tone> = { verified_public: "ok", observed: "info", inferred: "neutral", ai_estimate: "warn" };

/** Every statement about a competitor carries this label. An AI estimate is never shown without it. */
export function EvidenceBadge({ evidence }: { evidence: string }) {
  const { t } = useTranslation();
  return <Badge tone={tone[evidence] ?? "neutral"}>{t(`competitors.evidence.${evidence}`, { defaultValue: evidence })}</Badge>;
}

export function EvidenceKey() {
  const { t } = useTranslation();
  return (
    <details className="text-xs text-ink-soft">
      <summary className="cursor-pointer">{t("competitors.evidenceKey")}</summary>
      <ul className="mt-2 space-y-1">
        {["verified_public", "observed", "inferred", "ai_estimate"].map((e) => (
          <li key={e} className="flex items-start gap-2"><EvidenceBadge evidence={e} /><span>{t(`competitors.evidenceHelp.${e}`)}</span></li>
        ))}
      </ul>
    </details>
  );
}

const runTone: Record<string, Tone> = { completed: "ok", partial: "warn", failed: "danger", cancelled: "neutral", running: "info", queued: "neutral", ready: "ok", generating: "info",
  allowed: "ok", disallowed_by_robots: "warn", blocked: "danger", error: "danger", unchecked: "neutral" };

export function StateBadge({ state }: { state: string }) {
  const { t } = useTranslation();
  return <Badge tone={runTone[state] ?? "neutral"}>{t(`competitors.state.${state}`, { defaultValue: state })}</Badge>;
}
