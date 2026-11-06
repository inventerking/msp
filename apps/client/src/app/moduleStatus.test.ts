import { describe, expect, it } from "vitest";
import { modules } from "../modules";
import en from "../locales/en.json";
import { navState, statusTone } from "./moduleStatus";

describe("navState", () => {
  it("opens only active modules", () => {
    expect(navState("active")).toBe("open");
    for (const s of ["inactive", "pending", undefined]) expect(navState(s)).toBe("locked");
    for (const s of ["suspended", "expired", "revoked"]) expect(navState(s)).toBe("attention");
  });
});

describe("statusTone", () => {
  it("never shows a problem status as healthy", () => {
    for (const s of ["suspended", "expired", "revoked", "inactive", "pending"]) expect(statusTone(s)).not.toBe("ok");
  });
});

describe("module manifests", () => {
  it("cover all ten modules with unique keys and routes", () => {
    expect(modules).toHaveLength(10);
    expect(new Set(modules.map((m) => m.key)).size).toBe(10);
    expect(new Set(modules.map((m) => m.path)).size).toBe(10);
  });

  it("each have text in the catalog", () => {
    const names = en.modules as Record<string, unknown>;
    for (const m of modules) expect(names[m.key], m.key).toBeTruthy();
  });
});
