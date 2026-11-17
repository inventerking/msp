import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import "../../../app/i18n";
import type { Recommendation } from "../api/expansion";
import { RecommendationCard } from "./RecommendationCard";

afterEach(cleanup);

const base: Recommendation = {
  id: "r1", customer: { id: "c1", name: "Acme" }, kind: "confirm_coverage", service: { id: "s1", name: "Backup" },
  rationale: "Confirm whether Acme has Backup. Your records do not say.", ai_worded: false,
  evidence: [{ kind: "coverage_unknown", detail: { service: "Backup" } }], evidence_class: "inferred", status: "open", created_at: "2026-11-01T00:00:00Z",
};

function show(r: Recommendation) {
  render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><RecommendationCard r={r} /></MemoryRouter></QueryClientProvider>);
}

describe("recommendation card", () => {
  it("shows unknown coverage as a question, with no estimate and nothing to sell", () => {
    show(base);
    expect(screen.getByText("Confirm coverage")).toBeTruthy();
    expect(screen.getByText(/Nothing is recorded for Backup/)).toBeTruthy();
    expect(screen.queryByText("Estimate")).toBeNull();
    expect(screen.queryByText("Accept and open a deal")).toBeNull();
  });

  it("labels an estimated value as an estimate", () => {
    show({ ...base, kind: "cross_sell", estimated_mrr_minor: 12000, currency: "GBP", evidence_class: "observed", rationale: "Acme does not have Backup." });
    expect(screen.getByText("Estimate")).toBeTruthy();
  });
});
