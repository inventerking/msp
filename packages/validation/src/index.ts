import { z } from "zod";

// These give immediate feedback and mirror the API contract. The server remains the authority.
// Messages are translation keys, resolved by the app.

export const email = z.string().trim().min(1, "validation.required").max(254).email("validation.email");
export const newPassword = z.string().min(12, "validation.passwordLength").max(256, "validation.passwordLength");
const required = z.string().trim().min(1, "validation.required");

export const loginSchema = z.object({ email, password: required });
export const registerSchema = z.object({ display_name: required.max(120), email, password: newPassword });
export const emailOnlySchema = z.object({ email });
export const resetSchema = z.object({ password: newPassword });
export const changePasswordSchema = z.object({ current_password: required, new_password: newPassword });
export const recoverAdminSchema = z.object({ new_password: newPassword, reason: z.string().trim().min(3, "validation.tooShort").max(500) });
export const mfaCodeSchema = z.object({ code: z.string().trim().min(6, "validation.mfaCode").max(32, "validation.mfaCode") });
export const workspaceSchema = z.object({ name: z.string().trim().min(2, "validation.tooShort").max(120) });
export const inviteSchema = z.object({ email, role: required });
export const settingsSchema = z.object({
  name: z.string().trim().min(2, "validation.tooShort").max(120),
  legal_name: z.string().trim().max(200),
  website_domain: z.string().trim().max(253),
  country_code: z.union([z.literal(""), z.string().trim().length(2, "validation.countryCode")]),
  default_timezone: required.max(64),
  reporting_currency: z.string().trim().length(3, "validation.currencyCode"),
});

const key = z.string().regex(/^[a-z0-9_]{2,64}$/, "validation.key");
export const createAdminSchema = z.object({ display_name: required.max(120), email, password: newPassword, role: required });
export const productSchema = z.object({
  key,
  kind: z.enum(["module", "bundle", "suite", "addon"]),
  name: z.string().trim().min(2, "validation.tooShort").max(120),
  description: z.string().trim().max(2000),
  module_keys: z.array(z.string()).min(1, "validation.pickModule"),
});
export const planSchema = z.object({
  product_version_id: required,
  key,
  name: z.string().trim().min(2, "validation.tooShort").max(120),
  billing_period: z.enum(["monthly", "annual", "custom"]),
  trial_days: z.coerce.number().int().min(0).max(365),
  /** Members allowed per unit bought; empty means no limit. */
  seats: z.union([z.literal(""), z.coerce.number().int().min(1).max(100000)]).optional(),
  ai_credits: z.union([z.literal(""), z.coerce.number().int().min(0).max(10000000)]).optional(),
  lead_credits: z.union([z.literal(""), z.coerce.number().int().min(0).max(10000000)]).optional(),
});
export const priceSchema = z.object({ currency: z.string().trim().length(3, "validation.currencyCode"), amount_minor: z.coerce.number().int().min(0) });
