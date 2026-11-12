import { unwrap } from "@msp/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { assessmentApi, tenantKey, useMe } from "../../../app/api";

export function useAKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "it_assessment", ...rest);
}

export function useAMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useAKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

const working = new Set(["queued", "running"]);

export function useAssessments() {
  const key = useAKey();
  return useQuery({
    queryKey: key("list"),
    queryFn: () => unwrap(assessmentApi.GET("/it-assessments/assessments")),
    refetchInterval: (q) => (q.state.data?.data.some((a) => working.has(a.status)) ? 1500 : false),
  });
}

export function useAssessment(id: string) {
  const key = useAKey();
  return useQuery({
    queryKey: key("assessment", id),
    queryFn: () => unwrap(assessmentApi.GET("/it-assessments/assessments/{id}", { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data && working.has(q.state.data.status) ? 1500 : false),
  });
}

export function useAuthorizations() {
  const key = useAKey();
  return useQuery({ queryKey: key("authorizations"), queryFn: () => unwrap(assessmentApi.GET("/it-assessments/authorizations")) });
}

export function useTemplates() {
  const key = useAKey();
  return useQuery({ queryKey: key("templates"), queryFn: () => unwrap(assessmentApi.GET("/it-assessments/templates")) });
}

export function useVerifications() {
  const key = useAKey();
  return useQuery({ queryKey: key("verifications"), queryFn: () => unwrap(assessmentApi.GET("/it-assessments/domain-verifications")) });
}
