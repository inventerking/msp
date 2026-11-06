import { ApiError, createAdminApi, CsrfStore, unwrap } from "@msp/api-client";
import type { AdminComponents } from "@msp/types";
import { QueryClient, useQuery } from "@tanstack/react-query";
import type { TFunction } from "i18next";

export type AdminContext = AdminComponents["schemas"]["AdminContext"];

export const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2, staleTime: 10_000 } },
});

const csrf = new CsrfStore();

export const { client: api } = createAdminApi(csrf, { onUnauthorized: () => void queryClient.invalidateQueries({ queryKey: ["admin"] }) });

export async function fetchAdmin(): Promise<AdminContext | null> {
  try {
    const me = await unwrap(api.GET("/me"));
    csrf.token = me.csrf_token;
    return me;
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null;
    throw e;
  }
}

export function useAdmin() {
  return useQuery({ queryKey: ["admin"], queryFn: fetchAdmin, staleTime: 60_000 });
}

export function errorText(t: TFunction, e: unknown): string {
  const code = e instanceof ApiError ? e.code : "internal_error";
  return t(`errors.${code}`, { defaultValue: t("errors.internal_error") });
}

/** Mirrors the server's role rules, to hide actions a role cannot take. The server still decides. */
export function hasRole(me: AdminContext | null | undefined, ...roles: string[]) {
  return Boolean(me?.admin.roles.some((r) => roles.includes(r)));
}
