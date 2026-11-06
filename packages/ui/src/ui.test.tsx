import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Field, Input } from "./components/form";
import { Badge } from "./components/patterns";
import { sanitizeRichText } from "./sanitize";

afterEach(cleanup);

describe("sanitizeRichText", () => {
  it("removes scripts, event handlers and dangerous links", () => {
    const out = sanitizeRichText(`<p onclick="x()">ok</p><script>alert(1)</script><a href="javascript:alert(1)">x</a><img src=x onerror=y>`);
    expect(out).not.toMatch(/script|onclick|javascript:|onerror|<img/i);
    expect(out).toContain("<p>ok</p>");
  });

  it("keeps safe links", () => {
    expect(sanitizeRichText(`<a href="https://example.test">x</a>`)).toContain(`href="https://example.test"`);
  });
});

describe("Field", () => {
  it("connects the label and error to the control", () => {
    render(<Field label="Email" error="Required">{(ids) => <Input {...ids} />}</Field>);
    const input = screen.getByLabelText("Email");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(document.getElementById(input.getAttribute("aria-describedby") ?? "")?.textContent).toBe("Required");
  });
});

describe("Badge", () => {
  it("states its meaning in text", () => {
    render(<Badge tone="danger">Suspended</Badge>);
    expect(screen.getByText("Suspended")).toBeTruthy();
  });
});
