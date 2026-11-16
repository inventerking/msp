import { unwrap } from "@msp/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { tenantKey, useMe, visitorApi } from "../../../app/api";

export function useVKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "visitor_intelligence", ...rest);
}

export function useVMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useVKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useCan(permission: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(permission));
}

export function useSites() {
  const key = useVKey();
  return useQuery({ queryKey: key("sites"), queryFn: () => unwrap(visitorApi.GET("/visitor-intelligence/sites")) });
}

export function useSite(id: string) {
  const key = useVKey();
  return useQuery({ queryKey: key("site", id), queryFn: () => unwrap(visitorApi.GET("/visitor-intelligence/sites/{id}", { params: { path: { id } } })) });
}

export function useGoals(id: string) {
  const key = useVKey();
  return useQuery({ queryKey: key("goals", id), queryFn: () => unwrap(visitorApi.GET("/visitor-intelligence/sites/{id}/goals", { params: { path: { id } } })) });
}

export function useReport(id: string, days: number) {
  const key = useVKey();
  return useQuery({ queryKey: key("report", id, days), refetchInterval: 15_000, queryFn: () => unwrap(visitorApi.GET("/visitor-intelligence/sites/{id}/report", { params: { path: { id }, query: { days } } })) });
}
