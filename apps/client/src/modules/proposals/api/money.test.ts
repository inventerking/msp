import { describe, expect, it } from "vitest";
import { fromMinor, money, toMinor } from "./proposals";

describe("money", () => {
  it("converts typed amounts to minor units for each currency", () => {
    expect(toMinor("20.00", "GBP")).toBe(2000);
    expect(toMinor("19.995", "GBP")).toBe(2000);
    expect(toMinor("1500", "JPY")).toBe(1500); // No minor unit.
    expect(toMinor("-5", "GBP")).toBe(0);
    expect(toMinor("abc", "GBP")).toBe(0);
  });

  it("round-trips minor units for editing", () => {
    expect(fromMinor(90000, "GBP")).toBe("900.00");
    expect(fromMinor(1500, "JPY")).toBe("1500");
  });

  it("shows amounts with the currency", () => {
    expect(money(3290000, "GBP")).toContain("32,900.00");
    expect(money(undefined, "GBP")).toBe("—");
  });
});
