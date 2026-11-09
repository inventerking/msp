import createClient, { type Middleware } from "openapi-fetch";
import type { AdminPaths, CompetitorPaths, LeadPaths, ModulePaths, TenantPaths } from "@msp/types";

/** A failed API call. Code branches on `code`, which never changes once released. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly requestId?: string,
    readonly fields: { field: string; code: string }[] = [],
    readonly moduleKey?: string,
    readonly moduleStatus?: string,
  ) {
    super(code);
    this.name = "ApiError";
  }
}

export function toApiError(status: number, body: unknown): ApiError {
  const p = (body ?? {}) as { code?: string; request_id?: string; errors?: { field: string; code: string }[]; module_key?: string; module_status?: string };
  return new ApiError(status, p.code ?? "internal_error", p.request_id, p.errors ?? [], p.module_key, p.module_status);
}

/** Returns the data of a call, or throws ApiError. */
export async function unwrap<T>(call: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await call;
  if (error !== undefined || !response.ok) throw toApiError(response.status, error);
  return data as T;
}

const unsafe = new Set(["POST", "PUT", "PATCH", "DELETE"]);

export interface ApiHooks {
  /** Called on any 401, so the app can return to sign-in. */
  onUnauthorized?: () => void;
  /** Called on module_not_entitled, so the app can refresh what the workspace can use. */
  onNotEntitled?: () => void;
}

/** Holds the session's CSRF token, which only ever lives in memory. */
export class CsrfStore {
  token = "";
}

function middleware(csrf: CsrfStore, hooks: ApiHooks): Middleware {
  return {
    onRequest({ request }) {
      if (unsafe.has(request.method) && csrf.token) request.headers.set("X-CSRF-Token", csrf.token);
      return request;
    },
    async onResponse({ response, schemaPath }) {
      if (response.status === 401 && !schemaPath.startsWith("/auth/login") && !schemaPath.includes("mfa")) hooks.onUnauthorized?.();
      if (response.status === 403) {
        const body = (await response.clone().json().catch(() => null)) as { code?: string } | null;
        if (body?.code === "module_not_entitled") hooks.onNotEntitled?.();
      }
      return response;
    },
  };
}

export function createTenantApi(csrf: CsrfStore, hooks: ApiHooks = {}) {
  const client = createClient<TenantPaths>({ baseUrl: "/api/v1", credentials: "same-origin" });
  client.use(middleware(csrf, hooks));
  const modules = createClient<ModulePaths>({ baseUrl: "/api/v1", credentials: "same-origin" });
  modules.use(middleware(csrf, hooks));
  const leads = createClient<LeadPaths>({ baseUrl: "/api/v1", credentials: "same-origin" });
  leads.use(middleware(csrf, hooks));
  const competitors = createClient<CompetitorPaths>({ baseUrl: "/api/v1", credentials: "same-origin" });
  competitors.use(middleware(csrf, hooks));
  return { client, modules, leads, competitors };
}

export function createAdminApi(csrf: CsrfStore, hooks: ApiHooks = {}) {
  const client = createClient<AdminPaths>({ baseUrl: "/api/v1/admin", credentials: "same-origin" });
  client.use(middleware(csrf, hooks));
  return { client };
}

/** Fetches a file the API serves, such as an export, with the session's cookie. */
export async function fetchFile(path: string): Promise<Blob> {
  const res = await fetch(`/api/v1${path}`, { credentials: "same-origin" });
  if (!res.ok) {
    let body: unknown;
    try {
      body = await res.json();
    } catch {
      body = undefined;
    }
    throw toApiError(res.status, body);
  }
  return res.blob();
}

/** Saves a blob through the browser's download prompt. */
export function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
