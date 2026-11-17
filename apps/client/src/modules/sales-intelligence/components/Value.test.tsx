import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import "../../../app/i18n";
import { formatValue } from "../api/intel";
import { BarChart } from "./BarChart";
import { KindBadge, Num } from "./Value";

afterEach(cleanup);

describe("value kinds", () => {
  it("labels every kind in words, not only by colour", () => {
    render(<><KindBadge kind="actual" /><KindBadge kind="estimated" /><KindBadge kind="forecast" /></>);
    expect(screen.getByText("Actual")).toBeTruthy();
    expect(screen.getByText("Estimate")).toBeTruthy();
    expect(screen.getByText("Forecast")).toBeTruthy();
  });

  it("styles forecast numbers apart from actual ones", () => {
    render(<><Num kind="actual">10</Num><Num kind="forecast">20</Num></>);
    expect(screen.getByText("10").className).not.toContain("italic");
    expect(screen.getByText("20").className).toContain("italic");
  });

  it("names each bar's kind in the chart's data table", () => {
    render(<BarChart title="MRR" unit="money" currency="GBP" bars={[{ period: "2026-10-01", value: 100000, kind: "actual" }, { period: "2026-11-01", value: 120000, low: 90000, high: 150000, kind: "forecast" }]} />);
    const rows = screen.getAllByRole("row");
    expect(rows[1]?.textContent).toContain("Actual");
    expect(rows[2]?.textContent).toContain("Forecast");
  });
});

describe("formatValue", () => {
  it("formats by unit", () => {
    expect(formatValue("ratio", 0.4)).toMatch(/40\s?%/);
    expect(formatValue("count", 1234)).toMatch(/1.?234/);
    expect(formatValue("money", 125000, "GBP")).toContain("1,250");
    expect(formatValue("money", null, "GBP")).toBe("—");
  });
});
