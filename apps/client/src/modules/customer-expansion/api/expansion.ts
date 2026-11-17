import { unwrap } from "@msp/api-client";
import type { ExpansionComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, expansionApi, tenantKey, useMe } from "../../../app/api";

export type Customer = ExpansionComponents["schemas"]["Customer"];
export type Recommendation = ExpansionComponents["schemas"]["Recommendation"];
export type Contract = ExpansionComponents["schemas"]["Contract"];
export type CoverageLine = ExpansionComponents["schemas"]["CoverageLine"];

export function useXKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "customer_expansion", ...rest);
}

/** A mutation that refreshes everything Customer Expansion shows once it succeeds. */
export function useXMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useXKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useCan(permission: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(permission));
}

export function useCustomers(health?: string) {
  const key = useXKey();
  return useQuery({
    queryKey: key("customers", health ?? "all"),
    queryFn: () => unwrap(expansionApi.GET("/customer-expansion/customers", { params: { query: health ? { health: health as Customer["health_status"] } : {} } })),
  });
}

export function useCustomer(id: string) {
  const key = useXKey();
  return useQuery({ queryKey: key("customer", id), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/customers/{id}", { params: { path: { id } } })) });
}

export function useContracts(id: string) {
  const key = useXKey();
  return useQuery({ queryKey: key("contracts", id), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/customers/{id}/contracts", { params: { path: { id } } })) });
}

export function useCoverage(id: string) {
  const key = useXKey();
  return useQuery({ queryKey: key("coverage", id), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/customers/{id}/services", { params: { path: { id } } })) });
}

export function useRecommendations(query: { status?: Recommendation["status"]; customer_id?: string }) {
  const key = useXKey();
  return useQuery({ queryKey: key("recommendations", query), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/recommendations", { params: { query } })) });
}

export function useRenewals(status?: "open" | "acknowledged" | "resolved") {
  const key = useXKey();
  return useQuery({ queryKey: key("renewals", status ?? "all"), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/renewals", { params: { query: status ? { status } : {} } })) });
}

export function useOpportunities(status?: "open" | "in_deal" | "won" | "lost") {
  const key = useXKey();
  return useQuery({ queryKey: key("opportunities", status ?? "all"), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/opportunities", { params: { query: status ? { status } : {} } })) });
}

export function useOpportunitySummary() {
  const key = useXKey();
  return useQuery({ queryKey: key("opportunity-summary"), queryFn: () => unwrap(expansionApi.GET("/customer-expansion/opportunities/summary")) });
}

/** The tenant's service list, which lives in workspace settings. */
export function useServices() {
  const { data: me } = useMe();
  return useQuery({ queryKey: tenantKey(me, "services"), queryFn: () => unwrap(api.GET("/tenant/services")) });
}

/** Shows minor units in a currency, using the browser's formatting for that currency. */
export function money(minor: number | undefined | null, currency: string | undefined): string {
  if (minor === undefined || minor === null || !currency) return "—";
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(minor / 10 ** digits);
}

/** Converts a typed major amount to minor units. */
export function toMinor(major: string, currency: string): number | undefined {
  if (major.trim() === "") return undefined;
  const n = Number(major);
  if (!Number.isFinite(n) || n < 0) return undefined;
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  return Math.round(n * 10 ** digits);
}

export const healthTone = (h: string) => (h === "healthy" ? "ok" : h === "watch" ? "warn" : h === "at_risk" ? "danger" : "neutral");
