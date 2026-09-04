import { describe, expect, it } from "vitest";
import { sanitizeRedirect } from "./loginRedirect";

/**
 * `rd` arrives from the query string, so every case here is a link somebody
 * could send. The ones that must not survive are the ones that leave the origin.
 */
describe("sanitizeRedirect", () => {
  it("keeps a same-origin path", () => {
    expect(sanitizeRedirect("/agents/kagent/k8s-agent/chat")).toBe(
      "/agents/kagent/k8s-agent/chat",
    );
  });

  it("keeps the query and fragment with it", () => {
    expect(sanitizeRedirect("/agents/foo?tab=logs#latest")).toBe(
      "/agents/foo?tab=logs#latest",
    );
  });

  it("falls back to the front door when there is no destination", () => {
    expect(sanitizeRedirect(undefined)).toBe("/");
    expect(sanitizeRedirect(null)).toBe("/");
    expect(sanitizeRedirect("")).toBe("/");
  });

  it("rejects an absolute URL", () => {
    expect(sanitizeRedirect("https://evil.example.com/phish")).toBe("/");
  });

  it("rejects a protocol-relative URL", () => {
    expect(sanitizeRedirect("//evil.example.com/phish")).toBe("/");
  });

  it("rejects a backslash the URL Standard reads as a second slash", () => {
    expect(sanitizeRedirect("/\\evil.example.com/phish")).toBe("/");
  });

  it("rejects a tab-smuggled protocol-relative URL", () => {
    // The parser strips the tab before resolving, so this is `//evil...`.
    expect(sanitizeRedirect("/\t/evil.example.com/phish")).toBe("/");
  });

  it("rejects a dot segment that normalizes back into a protocol-relative path", () => {
    // Same-origin to the parser, but `.` is resolved away and what comes out is
    // `//evil.example.com/phish` — protocol-relative again for whoever reads it next.
    expect(sanitizeRedirect("/.//evil.example.com/phish")).toBe("/");
    expect(sanitizeRedirect("/a/../..//evil.example.com/phish")).toBe("/");
    expect(sanitizeRedirect("/./\\evil.example.com/phish")).toBe("/");
  });

  it("rejects a different scheme entirely", () => {
    expect(sanitizeRedirect("javascript:alert(1)")).toBe("/");
  });

  it("treats a bare host with no leading slash as a path segment", () => {
    // Matches URL semantics: with no scheme and no leading "/", this resolves
    // against the current path rather than naming a new host.
    expect(sanitizeRedirect("evil.example.com/phish")).toBe("/evil.example.com/phish");
  });
});
