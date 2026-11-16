import { unwrap } from "@msp/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { outreachApi, tenantKey, useMe } from "../../../app/api";

export function useOKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "outreach", ...rest);
}

/** A mutation that refreshes everything Outreach shows once it succeeds. */
export function useOMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useOKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useCan(permission: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(permission));
}

export function useDomains() {
  const key = useOKey();
  return useQuery({ queryKey: key("domains"), queryFn: () => unwrap(outreachApi.GET("/outreach/domains")) });
}

export function useSenders() {
  const key = useOKey();
  return useQuery({ queryKey: key("senders"), queryFn: () => unwrap(outreachApi.GET("/outreach/senders")) });
}

export function useTemplates() {
  const key = useOKey();
  return useQuery({ queryKey: key("templates"), queryFn: () => unwrap(outreachApi.GET("/outreach/templates")) });
}

export function useAudiences() {
  const key = useOKey();
  return useQuery({ queryKey: key("audiences"), queryFn: () => unwrap(outreachApi.GET("/outreach/audiences")) });
}

export function useCampaigns() {
  const key = useOKey();
  return useQuery({ queryKey: key("campaigns"), queryFn: () => unwrap(outreachApi.GET("/outreach/campaigns")) });
}

export function useCampaign(id: string) {
  const key = useOKey();
  return useQuery({
    queryKey: key("campaign", id),
    queryFn: () => unwrap(outreachApi.GET("/outreach/campaigns/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.status === "running" ? 5000 : false),
  });
}

export function usePreflight(id: string, enabled: boolean) {
  const key = useOKey();
  return useQuery({ queryKey: key("preflight", id), enabled, queryFn: () => unwrap(outreachApi.GET("/outreach/campaigns/{id}/preflight", { params: { path: { id } } })) });
}

export function useMessages(campaign?: string) {
  const key = useOKey();
  return useQuery({
    queryKey: key("messages", campaign ?? "all"),
    queryFn: () => unwrap(outreachApi.GET("/outreach/messages", { params: { query: campaign ? { campaign_id: campaign } : {} } })),
  });
}

export function useReplies() {
  const key = useOKey();
  return useQuery({ queryKey: key("replies"), queryFn: () => unwrap(outreachApi.GET("/outreach/replies")) });
}

export function useSuppressions() {
  const key = useOKey();
  return useQuery({ queryKey: key("suppressions"), queryFn: () => unwrap(outreachApi.GET("/outreach/suppressions")) });
}

export function useApprovals(enabled: boolean) {
  const key = useOKey();
  return useQuery({ queryKey: key("approvals"), enabled, queryFn: () => unwrap(outreachApi.GET("/outreach/approvals")) });
}
