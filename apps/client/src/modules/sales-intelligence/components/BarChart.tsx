import { useTranslation } from "react-i18next";
import { formatValue, monthLabel, type Unit, type ValueKind } from "../api/intel";

export type Bar = { period: string; value?: number | null; low?: number; high?: number; kind: ValueKind };

/**
 * Monthly bars. Each bar is styled by its kind: actual is solid, estimated is lighter, and forecast
 * is hatched with its range drawn as a line, so no one mistakes a forecast for a result. The same
 * data is in a table for screen readers.
 */
export function BarChart({ title, bars, unit, currency }: { title: string; bars: Bar[]; unit: Unit; currency?: string }) {
  const { t } = useTranslation();
  const values = bars.flatMap((b) => [b.value ?? 0, b.high ?? 0, b.low ?? 0]);
  const max = Math.max(1, ...values.map((v) => Math.abs(v)));
  const hasNegative = bars.some((b) => (b.value ?? 0) < 0);
  const h = 140;
  const zero = hasNegative ? h / 2 : h;
  const scale = (v: number) => (Math.abs(v) / max) * (hasNegative ? h / 2 : h);
  const w = Math.max(24, Math.floor(560 / Math.max(1, bars.length)));
  return (
    <figure className="w-full">
      <figcaption className="mb-1 text-sm font-medium text-ink">{title}</figcaption>
      <svg role="img" aria-label={title} viewBox={`0 0 ${w * bars.length} ${h + 18}`} className="h-44 w-full" preserveAspectRatio="none">
        <defs>
          <pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="6" height="6" className="fill-warn-soft" />
            <line x1="0" y1="0" x2="0" y2="6" className="stroke-warn" strokeWidth="2" />
          </pattern>
        </defs>
        <line x1="0" y1={zero} x2={w * bars.length} y2={zero} className="stroke-line" />
        {bars.map((b, i) => {
          const v = b.value ?? 0;
          const bh = scale(v);
          const y = v >= 0 ? zero - bh : zero;
          const x = i * w + w * 0.15;
          const bw = w * 0.7;
          const fill = b.kind === "forecast" ? "url(#hatch)" : undefined;
          return (
            <g key={b.period}>
              <rect x={x} y={y} width={bw} height={Math.max(bh, b.value ? 1 : 0)} fill={fill} className={b.kind === "actual" ? "fill-brand" : b.kind === "estimated" ? "fill-info" : ""} opacity={b.kind === "estimated" ? 0.6 : 1}>
                <title>{`${monthLabel(b.period)}: ${formatValue(unit, b.value, currency)} (${t(`intelKinds.${b.kind}`)})`}</title>
              </rect>
              {b.low !== undefined && b.high !== undefined ? <line x1={x + bw / 2} x2={x + bw / 2} y1={zero - scale(b.high)} y2={zero - scale(b.low)} className="stroke-ink" strokeWidth="2" /> : null}
              <text x={x + bw / 2} y={h + 14} textAnchor="middle" className="fill-ink-soft text-[9px]">{monthLabel(b.period)}</text>
            </g>
          );
        })}
      </svg>
      <table className="sr-only">
        <caption>{title}</caption>
        <thead><tr><th>{t("intel.month")}</th><th>{t("intel.value")}</th><th>{t("intel.kind")}</th></tr></thead>
        <tbody>{bars.map((b) => <tr key={b.period}><td>{monthLabel(b.period)}</td><td>{formatValue(unit, b.value, currency)}{b.low !== undefined ? ` (${formatValue(unit, b.low, currency)} – ${formatValue(unit, b.high, currency)})` : ""}</td><td>{t(`intelKinds.${b.kind}`)}</td></tr>)}</tbody>
      </table>
    </figure>
  );
}
