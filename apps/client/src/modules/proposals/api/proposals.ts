import { unwrap } from "@msp/api-client";
import type { ProposalComponents } from "@msp/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { proposalApi, tenantKey, useMe } from "../../../app/api";

export type Version = ProposalComponents["schemas"]["Version"];
export type LineInput = ProposalComponents["schemas"]["LineInput"];
export type Totals = ProposalComponents["schemas"]["Totals"];

export function usePropKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "proposals", ...rest);
}

export function usePropMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = usePropKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useProposals() {
  const key = usePropKey();
  return useQuery({ queryKey: key("list"), queryFn: () => unwrap(proposalApi.GET("/proposals/proposals")) });
}

export function useProposal(id: string) {
  const key = usePropKey();
  return useQuery({ queryKey: key("proposal", id), queryFn: () => unwrap(proposalApi.GET("/proposals/proposals/{id}", { params: { path: { id } } })) });
}

export function useVersion(id: string, version: number | undefined) {
  const key = usePropKey();
  return useQuery({
    queryKey: key("proposal", id, "version", version),
    enabled: version !== undefined,
    queryFn: () => unwrap(proposalApi.GET("/proposals/proposals/{id}/versions/{version}", { params: { path: { id, version: version ?? 1 } } })),
    refetchInterval: (q) => (q.state.data?.status === "issued" && !q.state.data.has_pdf ? 2000 : false),
  });
}

export function useActivity(id: string) {
  const key = usePropKey();
  return useQuery({ queryKey: key("proposal", id, "activity"), queryFn: () => unwrap(proposalApi.GET("/proposals/proposals/{id}/activity", { params: { path: { id } } })) });
}

export function useCatalog() {
  const key = usePropKey();
  return useQuery({ queryKey: key("catalog"), queryFn: () => unwrap(proposalApi.GET("/proposals/catalog")) });
}

export function useTextTemplates() {
  const key = usePropKey();
  return useQuery({ queryKey: key("text-templates"), queryFn: () => unwrap(proposalApi.GET("/proposals/text-templates")) });
}

/** Minor units shown in a currency, with the browser's formatting. */
export function money(minor: number | undefined, currency: string): string {
  if (minor === undefined) return "—";
  const f = new Intl.NumberFormat(undefined, { style: "currency", currency });
  const digits = f.resolvedOptions().maximumFractionDigits ?? 2;
  return f.format(minor / 10 ** digits);
}

export function toMinor(major: string, currency: string): number {
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  const n = Number(major);
  return Number.isFinite(n) && n >= 0 ? Math.round(n * 10 ** digits) : 0;
}

export function fromMinor(minor: number, currency: string): string {
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  return (minor / 10 ** digits).toFixed(digits);
}
