import { describe, expect, it } from "vitest";
import en from "../locales/en.json";
import { hasRole, type AdminContext } from "./api";

const me = (roles: string[]): AdminContext => ({ admin: { id: "a", email: "a@b.co", display_name: "A", status: "active", roles }, csrf_token: "c", mfa_verified: true });

describe("hasRole", () => {
  it("matches any listed role", () => {
    expect(hasRole(me(["billing_admin"]), "super_admin", "billing_admin")).toBe(true);
    expect(hasRole(me(["platform_analyst"]), "super_admin")).toBe(false);
    expect(hasRole(null, "super_admin")).toBe(false);
  });
});

describe("catalog", () => {
  it("names all five platform roles", () => {
    expect(Object.keys(en.admins.roleNames).sort()).toEqual(["billing_admin", "integration_admin", "platform_analyst", "platform_support", "super_admin"]);
  });
});
