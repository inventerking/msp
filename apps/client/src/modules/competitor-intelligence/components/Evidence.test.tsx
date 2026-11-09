import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import "../../../app/i18n";
import { EvidenceBadge, EvidenceKey } from "./Evidence";

afterEach(cleanup);

describe("evidence labels", () => {
  it("labels an AI estimate as one, in words", () => {
    render(<EvidenceBadge evidence="ai_estimate" />);
    expect(screen.getByText("AI estimate")).toBeTruthy();
  });

  it("explains every label", () => {
    render(<EvidenceKey />);
    for (const label of ["Verified", "Observed", "Inferred", "AI estimate"]) expect(screen.getByText(label)).toBeTruthy();
    expect(screen.getByText(/may be wrong/)).toBeTruthy();
  });
});
