import type { Tone } from "@msp/ui";

export type NavState = "open" | "locked" | "attention";

/** How a module appears in navigation for each entitlement status. Presentation only. */
export function navState(status: string | undefined): NavState {
  if (status === "active") return "open";
  if (status === "suspended" || status === "revoked" || status === "expired") return "attention";
  return "locked";
}

export function statusTone(status: string | undefined): Tone {
  switch (status) {
    case "active":
      return "ok";
    case "pending":
      return "info";
    case "suspended":
    case "expired":
      return "warn";
    case "revoked":
      return "danger";
    default:
      return "neutral";
  }
}
