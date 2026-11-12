import { unwrap } from "@msp/api-client";
import type { CrmComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { crmApi, tenantKey, useMe } from "../../../app/api";

export type Deal = CrmComponents["schemas"]["Deal"];
export type Stage = CrmComponents["schemas"]["Stage"];

export function useCrmKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "crm", ...rest);
}

/** A mutation that refreshes the module when it succeeds. */
export function useCrmMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useCrmKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function usePipelines() {
  const key = useCrmKey();
  return useQuery({ queryKey: key("pipelines"), queryFn: () => unwrap(crmApi.GET("/crm/pipelines")) });
}

export function useBoard(pipeline: string | undefined) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("board", pipeline), enabled: Boolean(pipeline), queryFn: () => unwrap(crmApi.GET("/crm/pipelines/{id}/board", { params: { path: { id: pipeline ?? "" } } })) });
}

export function useCompanies(q: string) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("companies", q), queryFn: () => unwrap(crmApi.GET("/crm/companies", { params: { query: { limit: 100, ...(q ? { q } : {}) } } })) });
}

export function useCompany(id: string) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("company", id), queryFn: () => unwrap(crmApi.GET("/crm/companies/{id}", { params: { path: { id } } })) });
}

export function useContacts(company?: string) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("contacts", company), queryFn: () => unwrap(crmApi.GET("/crm/contacts", { params: { query: company ? { crm_company_id: company } : {} } })) });
}

export function useDeals(filter: { crm_company_id?: string; status?: "open" | "won" | "lost" }) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("deals", filter), queryFn: () => unwrap(crmApi.GET("/crm/deals", { params: { query: filter } })) });
}

export function useDeal(id: string) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("deal", id), queryFn: () => unwrap(crmApi.GET("/crm/deals/{id}", { params: { path: { id } } })) });
}

export function useTasks(filter: { mine?: boolean; status?: "open" | "done" | "cancelled"; deal_id?: string; crm_company_id?: string }) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("tasks", filter), queryFn: () => unwrap(crmApi.GET("/crm/tasks", { params: { query: filter } })) });
}

export function useActivities(filter: { crm_company_id?: string; deal_id?: string }) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("activities", filter), queryFn: () => unwrap(crmApi.GET("/crm/activities", { params: { query: filter } })) });
}

export function useNotes(subjectType: "company" | "contact" | "deal", subjectId: string) {
  const key = useCrmKey();
  return useQuery({ queryKey: key("notes", subjectType, subjectId), queryFn: () => unwrap(crmApi.GET("/crm/notes", { params: { query: { subject_type: subjectType, subject_id: subjectId } } })) });
}

/** Shows minor units in a currency, using the browser's formatting for that currency. */
export function money(minor: number | undefined, currency: string): string {
  if (minor === undefined) return "—";
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(minor / 10 ** digits);
}

/** Converts a typed major amount to minor units for a currency. */
export function toMinor(major: string, currency: string): number | undefined {
  if (major.trim() === "") return undefined;
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  const n = Number(major);
  return Number.isFinite(n) && n >= 0 ? Math.round(n * 10 ** digits) : undefined;
}
