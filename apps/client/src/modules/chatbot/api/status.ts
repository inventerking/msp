import { unwrap } from "@msp/api-client";
import { useQuery } from "@tanstack/react-query";
import { moduleApi, tenantKey, useMe } from "../../../app/api";

export function useStatus() {
  const { data: me } = useMe();
  return useQuery({ queryKey: tenantKey(me, "chatbot", "status"), queryFn: () => unwrap(moduleApi.GET("/chatbot/status")) });
}
