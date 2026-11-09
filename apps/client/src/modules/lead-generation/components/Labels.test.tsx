import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import "../../../app/i18n";
import { LicenceBadge, ScoreBadge } from "./Labels";

afterEach(cleanup);

describe("lead labels", () => {
  it("never shows a score without how much is known", () => {
    render(<ScoreBadge score={63} coverage={0.75} />);
    expect(screen.getByText("63")).toBeTruthy();
    expect(screen.getByText("75% known")).toBeTruthy();
  });

  it("says when a prospect has no score", () => {
    render(<ScoreBadge score={undefined} coverage={undefined} />);
    expect(screen.getByText("Not scored")).toBeTruthy();
  });

  it("marks data licensed only to this workspace", () => {
    render(<LicenceBadge licence="licensed_tenant" />);
    expect(screen.getByText("Licensed to you")).toBeTruthy();
  });
});
