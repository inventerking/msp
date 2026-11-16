import { unwrap } from "@msp/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { chatbotApi, tenantKey, useMe } from "../../../app/api";

export function useCKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "chatbot", ...rest);
}

export function useCMutation<A, R>(fn: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  const key = useCKey();
  return useMutation({ mutationFn: fn, onSuccess: () => void qc.invalidateQueries({ queryKey: key() }) });
}

export function useCan(permission: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(permission));
}

export function useBots() {
  const key = useCKey();
  return useQuery({ queryKey: key("bots"), queryFn: () => unwrap(chatbotApi.GET("/chatbot/bots")) });
}

export function useBot(id: string) {
  const key = useCKey();
  return useQuery({ queryKey: key("bot", id), queryFn: () => unwrap(chatbotApi.GET("/chatbot/bots/{id}", { params: { path: { id } } })) });
}

export function useKnowledge(id: string) {
  const key = useCKey();
  return useQuery({ queryKey: key("knowledge", id), queryFn: () => unwrap(chatbotApi.GET("/chatbot/bots/{id}/knowledge", { params: { path: { id } } })) });
}

export function useQuestions(id: string) {
  const key = useCKey();
  return useQuery({ queryKey: key("questions", id), queryFn: () => unwrap(chatbotApi.GET("/chatbot/bots/{id}/questions", { params: { path: { id } } })) });
}

export function useConversations() {
  const key = useCKey();
  return useQuery({ queryKey: key("conversations"), refetchInterval: 10_000, queryFn: () => unwrap(chatbotApi.GET("/chatbot/conversations")) });
}

export function useConversation(id: string) {
  const key = useCKey();
  return useQuery({ queryKey: key("conversation", id), refetchInterval: 5_000, queryFn: () => unwrap(chatbotApi.GET("/chatbot/conversations/{id}", { params: { path: { id } } })) });
}

export function useHandoffs() {
  const key = useCKey();
  return useQuery({ queryKey: key("handoffs"), refetchInterval: 10_000, queryFn: () => unwrap(chatbotApi.GET("/chatbot/handoffs")) });
}
