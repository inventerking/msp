import { describe, expect, it } from "vitest";
import { loginSchema, planSchema, registerSchema, settingsSchema } from "./index";

describe("registerSchema", () => {
  it("requires the same password length as the API", () => {
    expect(registerSchema.safeParse({ display_name: "A", email: "a@b.co", password: "short" }).success).toBe(false);
    expect(registerSchema.safeParse({ display_name: "A", email: "a@b.co", password: "long-enough-pass" }).success).toBe(true);
  });

  it("rejects a malformed email", () => {
    expect(registerSchema.safeParse({ display_name: "A", email: "nope", password: "long-enough-pass" }).success).toBe(false);
  });
});

describe("loginSchema", () => {
  it("does not apply new-password rules to existing passwords", () => {
    expect(loginSchema.safeParse({ email: "a@b.co", password: "old" }).success).toBe(true);
  });
});

describe("settingsSchema", () => {
  it("allows an empty country but not a malformed one", () => {
    const base = { name: "Acme", legal_name: "", website_domain: "", default_timezone: "UTC", reporting_currency: "USD" };
    expect(settingsSchema.safeParse({ ...base, country_code: "" }).success).toBe(true);
    expect(settingsSchema.safeParse({ ...base, country_code: "GB" }).success).toBe(true);
    expect(settingsSchema.safeParse({ ...base, country_code: "GBR" }).success).toBe(false);
  });
});

describe("planSchema", () => {
  it("coerces numbers from form inputs and bounds trial days", () => {
    const base = { product_version_id: "x", key: "plan_a", name: "Plan", billing_period: "monthly" };
    expect(planSchema.parse({ ...base, trial_days: "14" }).trial_days).toBe(14);
    expect(planSchema.safeParse({ ...base, trial_days: "999" }).success).toBe(false);
  });
});
