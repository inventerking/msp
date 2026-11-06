import { ApiError } from "@msp/api-client";
import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import type { TFunction } from "i18next";

/** Message text for a stable API error code. */
export function errorText(t: TFunction, e: unknown): string {
  const code = e instanceof ApiError ? e.code : "internal_error";
  return t(`errors.${code}`, { defaultValue: t("errors.internal_error") });
}

/** Puts server field errors on the form, and returns a message for anything else. */
export function applyServerError<T extends FieldValues>(t: TFunction, e: unknown, setError: UseFormSetError<T>, fields: string[]): string | undefined {
  if (e instanceof ApiError && e.fields.length > 0) {
    let placed = false;
    for (const f of e.fields) {
      if (fields.includes(f.field)) {
        setError(f.field as Path<T>, { message: t(`fieldErrors.${f.code}`, { defaultValue: t("fieldErrors.invalid") }) });
        placed = true;
      }
    }
    if (placed) return undefined;
  }
  return errorText(t, e);
}
