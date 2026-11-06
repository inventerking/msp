import { ApiError, createTenantApi, CsrfStore, unwrap } from "@msp/api-client";
import { QueryClient, useQuery } from "@tanstack/react-query";
import type { TenantComponents } from "@msp/types";

export type MeContext = TenantComponents["schemas"]["MeContext"];

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2,
      refetchOnWindowFocus: true,
      staleTime: 15_000,
    },
  },
});

const csrf = new CsrfStore();

export const { client: api, modules: moduleApi } = createTenantApi(csrf, {
  onUnauthorized: () => void queryClient.invalidateQueries({ queryKey: ["me"] }),
  onNotEntitled: () => void queryClient.invalidateQueries({ queryKey: ["me"] }),
});

export async function fetchMe(): Promise<MeContext | null> {
  try {
    const me = await unwrap(api.GET("/me/context"));
    csrf.token = me.csrf_token;
    return me;
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null;
    throw e;
  }
}

export function useMe() {
  return useQuery({ queryKey: ["me"], queryFn: fetchMe, staleTime: 30_000 });
}

/** Query keys start with the tenant, so one workspace's data can never appear in another. */
export function tenantKey(me: MeContext | null | undefined, ...rest: unknown[]) {
  return [me?.tenant?.id ?? "none", ...rest];
}

/** Forgets everything cached, for sign-out and workspace switches. */
export async function resetSession() {
  queryClient.clear();
  await queryClient.fetchQuery({ queryKey: ["me"], queryFn: fetchMe, staleTime: 0 });
}
