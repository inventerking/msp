import { Badge, cn } from "@msp/ui";
import { useTranslation } from "react-i18next";
import type { ValueKind } from "../api/intel";

/** Every number carries its kind. Estimates and forecasts look different from actual values. */
export function KindBadge({ kind }: { kind: ValueKind }) {
  const { t } = useTranslation();
  return <Badge tone={kind === "actual" ? "neutral" : kind === "estimated" ? "info" : "warn"}>{t(`intelKinds.${kind}`)}</Badge>;
}

/** A number styled by its kind: forecasts in italics with a dashed underline, estimates with a dotted one. */
export function Num({ kind, children, className }: { kind: ValueKind; children: string; className?: string }) {
  return (
    <span className={cn("tabular-nums", kind === "forecast" && "italic underline decoration-dashed underline-offset-4", kind === "estimated" && "underline decoration-dotted underline-offset-4", className)}>
      {children}
    </span>
  );
}

/** When the numbers were computed, and the latest activity they include. */
export function AsOf({ asOf, factsAsOf }: { asOf?: string; factsAsOf?: string }) {
  const { t } = useTranslation();
  if (!asOf) return <p className="text-xs text-ink-faint">{t("intel.notComputedYet")}</p>;
  return (
    <p className="text-xs text-ink-faint">
      {t("intel.asOf", { when: new Date(asOf).toLocaleString() })}
      {factsAsOf ? ` · ${t("intel.factsAsOf", { when: new Date(factsAsOf).toLocaleString() })}` : ""}
    </p>
  );
}

/** Why a section has nothing to show. */
export function Unavailable({ reason }: { reason?: string }) {
  const { t } = useTranslation();
  return <p className="rounded-md bg-sunken p-3 text-sm text-ink-soft">{t(`intel.unavailable.${reason ?? "module_not_active"}`)}</p>;
}

/** Shows what was left out of a value, such as amounts with no exchange rate. */
export function Flags({ flags }: { flags?: string[] }) {
  const { t } = useTranslation();
  if (!flags?.length) return null;
  return <span className="ml-1 text-xs text-warn">{flags.map((f) => t(`intel.flags.${f}`, { defaultValue: f })).join(" · ")}</span>;
}
