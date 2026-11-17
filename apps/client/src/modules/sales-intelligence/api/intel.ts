import { unwrap } from "@msp/api-client";
import type { SalesIntelComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, intelApi, tenantKey, useMe } from "../../../app/api";

export type View = SalesIntelComponents["schemas"]["AnalyticsView"];
export type Series = SalesIntelComponents["schemas"]["MetricSeries"];
export type Forecast = SalesIntelComponents["schemas"]["Forecast"];
export type ValueKind = SalesIntelComponents["schemas"]["ValueKind"];
export type Unit = SalesIntelComponents["schemas"]["Unit"];

export function useIKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "sales_intelligence", ...rest);
}

export function useIMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useIKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useCan(permission: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(permission));
}

export type Range = { from?: string; to?: string };

export function useDashboard(r: Range) {
  const key = useIKey();
  return useQuery({ queryKey: key("dashboard", r), queryFn: () => unwrap(intelApi.GET("/sales-intelligence/dashboard", { params: { query: r } })) });
}

type SectionPath = "/sales-intelligence/revenue" | "/sales-intelligence/leads" | "/sales-intelligence/campaigns" | "/sales-intelligence/funnel" | "/sales-intelligence/win-loss" | "/sales-intelligence/team" | "/sales-intelligence/costs-summary";

export function useSection(path: SectionPath, r: Range, enabled = true) {
  const key = useIKey();
  return useQuery({ queryKey: key("section", path, r), enabled, queryFn: () => unwrap(intelApi.GET(path, { params: { query: r } })) });
}

export function useDefinitions() {
  const key = useIKey();
  return useQuery({ queryKey: key("definitions"), staleTime: 3_600_000, queryFn: () => unwrap(intelApi.GET("/sales-intelligence/definitions")) });
}

export function useForecasts() {
  const key = useIKey();
  return useQuery({
    queryKey: key("forecasts"),
    queryFn: () => unwrap(intelApi.GET("/sales-intelligence/forecasts", { params: { query: {} } })),
    refetchInterval: (q) => (q.state.data?.data.some((f) => f.status === "queued" || f.status === "running") ? 2000 : false),
  });
}

export function useCosts(enabled: boolean) {
  const key = useIKey();
  return useQuery({ queryKey: key("costs"), enabled, queryFn: () => unwrap(intelApi.GET("/sales-intelligence/costs")) });
}

export function useReports() {
  const key = useIKey();
  return useQuery({ queryKey: key("reports"), queryFn: () => unwrap(intelApi.GET("/sales-intelligence/reports")) });
}

export function useInsights() {
  const key = useIKey();
  return useQuery({ queryKey: key("insights"), queryFn: () => unwrap(intelApi.GET("/sales-intelligence/insights")) });
}

/** Names for team members, when the viewer may see the member list. */
export function useMemberNames(enabled: boolean) {
  const { data: me } = useMe();
  return useQuery({
    queryKey: tenantKey(me, "members"),
    enabled,
    queryFn: async () => Object.fromEntries((await unwrap(api.GET("/tenant/members"))).data.map((m) => [m.user_id, m.display_name])) as Record<string, string>,
  });
}

/** Formats a value by its unit: money in the reporting currency, ratios as percentages, counts as numbers. */
export function formatValue(unit: Unit, value: number | null | undefined, currency?: string): string {
  if (value === null || value === undefined) return "—";
  if (unit === "ratio") return new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 1 }).format(value);
  if (unit === "money") {
    if (!currency) return String(value);
    const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
    return new Intl.NumberFormat(undefined, { style: "currency", currency, maximumFractionDigits: 0 }).format(value / 10 ** digits);
  }
  return new Intl.NumberFormat().format(value);
}

/** The series for one metric and dimension, if present. */
export function seriesOf(view: View | undefined, key: string, dimension = "all"): Series | undefined {
  return view?.series.find((s) => s.key === key && s.dimension === dimension);
}

/** The value of a series for one month, or the latest month when none is given. */
export function valueAt(s: Series | undefined, period?: string, dimValue?: string) {
  const points = (s?.points ?? []).filter((p) => (dimValue === undefined ? !p.dimension_value : p.dimension_value === dimValue));
  if (!points.length) return undefined;
  return period ? points.find((p) => p.period === period) : points[points.length - 1];
}

/** First days of the months in a range, from the view. */
export function monthsOf(view: View | undefined): string[] {
  if (!view) return [];
  const out: string[] = [];
  const d = new Date(`${view.from}T00:00:00Z`);
  const end = new Date(`${view.to}T00:00:00Z`);
  while (d <= end) {
    out.push(d.toISOString().slice(0, 10));
    d.setUTCMonth(d.getUTCMonth() + 1);
  }
  return out;
}

export function monthLabel(period: string): string {
  return new Date(`${period}T00:00:00Z`).toLocaleDateString(undefined, { month: "short", year: "2-digit", timeZone: "UTC" });
}
