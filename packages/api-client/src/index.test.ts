import { describe, expect, it } from "vitest";
import { toApiError, unwrap } from "./index";

describe("toApiError", () => {
  it("keeps the stable code and field errors", () => {
    const e = toApiError(422, { code: "validation_failed", request_id: "r1", errors: [{ field: "email", code: "invalid" }] });
    expect(e.code).toBe("validation_failed");
    expect(e.requestId).toBe("r1");
    expect(e.fields[0]?.field).toBe("email");
  });

  it("carries module status for the status page", () => {
    const e = toApiError(403, { code: "module_not_entitled", module_key: "crm", module_status: "suspended" });
    expect(e.moduleKey).toBe("crm");
    expect(e.moduleStatus).toBe("suspended");
  });

  it("falls back safely when the body is not a problem", () => {
    expect(toApiError(502, undefined).code).toBe("internal_error");
  });
});

describe("unwrap", () => {
  it("returns data on success", async () => {
    await expect(unwrap(Promise.resolve({ data: { a: 1 }, response: new Response(null, { status: 200 }) }))).resolves.toEqual({ a: 1 });
  });

  it("throws ApiError on failure", async () => {
    await expect(unwrap(Promise.resolve({ error: { code: "not_found" }, response: new Response(null, { status: 404 }) }))).rejects.toMatchObject({ status: 404, code: "not_found" });
  });
});
