import { describe, expect, it } from "vitest";
import {
  CONVERSATION_NAME_LIMIT,
  conversationName,
} from "./conversationName";

describe("conversationName", () => {
  it("uses the message as it was typed", () => {
    expect(conversationName("Why is checkout crashlooping?")).toBe(
      "Why is checkout crashlooping?",
    );
  });

  it("takes only the first line", () => {
    // A pasted stack trace would otherwise make one row as tall as the rail.
    expect(conversationName("Deployment failed\n\nat main.go:42\nat run.go:11")).toBe(
      "Deployment failed",
    );
  });

  it("trims a long line and marks that it did", () => {
    const long = "a".repeat(CONVERSATION_NAME_LIMIT + 20);

    const named = conversationName(long);

    expect(named).toHaveLength(CONVERSATION_NAME_LIMIT + 1);
    expect(named.endsWith("…")).toBe(true);
  });

  it("does not mark a name it did not have to trim", () => {
    const exact = "b".repeat(CONVERSATION_NAME_LIMIT);

    expect(conversationName(exact)).toBe(exact);
  });

  it("does not leave a space before the ellipsis", () => {
    const named = conversationName(`${"word ".repeat(20)}end`);

    expect(named).not.toContain(" …");
  });

  it("falls back when there is nothing to name it after", () => {
    // Not reachable from the composer, which refuses to send blank text — but a
    // name is required, and "" would render as an unclickable empty row.
    expect(conversationName("   \n  ")).toBe("New conversation");
  });
});
