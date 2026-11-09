import { unwrap } from "@msp/api-client";
import type { CompetitorComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { competitorApi, tenantKey, useMe } from "../../../app/api";

export type Competitor = CompetitorComponents["schemas"]["Competitor"];
export type Observation = CompetitorComponents["schemas"]["Observation"];
export type EvidenceClass = CompetitorComponents["schemas"]["EvidenceClass"];

export function useCiKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "competitor_intelligence", ...rest);
}

/** A mutation that refreshes the module when it succeeds. */
export function useCiMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useCiKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

const working = new Set(["queued", "running", "generating"]);

export function useCompetitors() {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitors"), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors")) });
}

export function useCompetitor(id: string) {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitor", id), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}", { params: { path: { id } } })) });
}

export function useRuns(id: string) {
  const key = useCiKey();
  return useQuery({
    queryKey: key("competitor", id, "runs"),
    queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}/research-runs", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.data.some((r) => working.has(r.status)) ? 1500 : false),
  });
}

export function useObservations(id: string, history: boolean) {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitor", id, "observations", history), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}/observations", { params: { path: { id }, query: { history } } })) });
}

export function useChanges(id: string) {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitor", id, "changes"), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}/changes", { params: { path: { id } } })) });
}

export function useSources(id: string) {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitor", id, "sources"), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}/sources", { params: { path: { id } } })) });
}

export function useConfig(id: string) {
  const key = useCiKey();
  return useQuery({ queryKey: key("competitor", id, "config"), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/competitors/{id}/research-config", { params: { path: { id } } })) });
}

export function useOpportunities() {
  const key = useCiKey();
  return useQuery({ queryKey: key("opportunities"), queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/opportunities")) });
}

export function useReports() {
  const key = useCiKey();
  return useQuery({
    queryKey: key("reports"),
    queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/reports")),
    refetchInterval: (q) => (q.state.data?.data.some((r) => working.has(r.status)) ? 2000 : false),
  });
}

export function useReport(id: string) {
  const key = useCiKey();
  return useQuery({
    queryKey: key("report", id),
    queryFn: () => unwrap(competitorApi.GET("/competitor-intelligence/reports/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data && working.has(q.state.data.status) ? 2000 : false),
  });
}
