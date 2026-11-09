import { unwrap } from "@msp/api-client";
import type { LeadComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, leadApi, tenantKey, useMe } from "../../../app/api";

export type Prospect = LeadComponents["schemas"]["Prospect"];
export type ProspectDetail = LeadComponents["schemas"]["ProspectDetail"];
export type Search = LeadComponents["schemas"]["Search"];
export type Criteria = LeadComponents["schemas"]["Criteria"];
export type Score = LeadComponents["schemas"]["Score"];
export type ProspectStatus = LeadComponents["schemas"]["ProspectStatus"];

/** Query keys for this module, always under the tenant. */
export function useLeadKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "lead_generation", ...rest);
}

/** Refreshes everything this module shows after a change. */
export function useInvalidateLeads() {
  const qc = useQueryClient();
  const key = useLeadKey();
  return () => qc.invalidateQueries({ queryKey: key() });
}

const running = new Set(["queued", "running", "validating", "importing", "generating"]);
/** Polls while a background job is still working, then stops. */
export const pollWhile = (status: string | undefined) => (status && running.has(status) ? 1500 : false);

export function useReference() {
  const { data: me } = useMe();
  return useQuery({ queryKey: tenantKey(me, "reference"), queryFn: () => unwrap(api.GET("/reference")), staleTime: 3_600_000 });
}

export function useProspects(filter: { status?: ProspectStatus; min_score?: number; list_id?: string; q?: string }) {
  const key = useLeadKey();
  return useQuery({ queryKey: key("prospects", filter), queryFn: () => unwrap(leadApi.GET("/lead-generation/prospects", { params: { query: { limit: 100, ...filter } } })) });
}

export function useProspect(id: string) {
  const key = useLeadKey();
  return useQuery({
    queryKey: key("prospect", id),
    queryFn: () => unwrap(leadApi.GET("/lead-generation/prospects/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.enrichments.some((e) => e.status === "queued" || e.status === "running") ? 1500 : false),
  });
}

/** The latest score. While an explanation is being written, it polls until the text arrives. */
export function useScore(id: string, awaitingExplanation = false) {
  const key = useLeadKey();
  return useQuery({
    queryKey: key("prospect", id, "score"),
    queryFn: () => unwrap(leadApi.GET("/lead-generation/prospects/{id}/score", { params: { path: { id } } })),
    retry: false,
    refetchInterval: (q) => (awaitingExplanation && q.state.data && !q.state.data.explanation ? 2000 : false),
  });
}

export function useSearches() {
  const key = useLeadKey();
  return useQuery({
    queryKey: key("searches"),
    queryFn: () => unwrap(leadApi.GET("/lead-generation/searches", { params: { query: { limit: 50 } } })),
    refetchInterval: (q) => (q.state.data?.data.some((s) => running.has(s.status)) ? 1500 : false),
  });
}

export function useSearchResults(id: string | undefined) {
  const key = useLeadKey();
  return useQuery({ queryKey: key("search", id, "results"), enabled: Boolean(id), queryFn: () => unwrap(leadApi.GET("/lead-generation/searches/{id}/results", { params: { path: { id: id ?? "" } } })) });
}

export function useLists() {
  const key = useLeadKey();
  return useQuery({ queryKey: key("lists"), queryFn: () => unwrap(leadApi.GET("/lead-generation/lists")) });
}

export function useIcp() {
  const key = useLeadKey();
  return useQuery({ queryKey: key("icp"), queryFn: () => unwrap(api.GET("/tenant/icp")) });
}

/** A mutation that refreshes the module when it succeeds. */
export function useLeadMutation<A, R>(fn: (a: A) => Promise<R>) {
  const invalidate = useInvalidateLeads();
  return useMutation({ mutationFn: fn, onSuccess: () => void invalidate() });
}
