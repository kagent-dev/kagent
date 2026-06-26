import { chunkText, cleanSessionTitle, connToChatStatus, toolResultText } from "@/lib/acp";
import type { SessionUpdate } from "@/types/acp";

describe("acp helpers", () => {
  describe("cleanSessionTitle", () => {
    it("strips a leading working-directory prefix", () => {
      expect(cleanSessionTitle("[Working directory: /home/agent] Fix the bug")).toBe("Fix the bug");
    });

    it("is case-insensitive and trims surrounding whitespace", () => {
      expect(cleanSessionTitle("  [working directory: /x]   Hello  ")).toBe("Hello");
    });

    it("leaves titles without the prefix unchanged (trimmed)", () => {
      expect(cleanSessionTitle("  Plain title  ")).toBe("Plain title");
    });
  });

  describe("chunkText", () => {
    it("returns the text of a content block", () => {
      expect(chunkText({ text: "hi" })).toBe("hi");
    });

    it("returns empty string for missing or non-string text", () => {
      expect(chunkText({ text: 42 })).toBe("");
      expect(chunkText({})).toBe("");
      expect(chunkText(null)).toBe("");
      expect(chunkText("nope")).toBe("");
    });
  });

  describe("toolResultText", () => {
    it("joins text parts from a content array", () => {
      const update: SessionUpdate = {
        content: [{ content: { text: "line1" } }, { content: { text: "line2" } }],
      };
      expect(toolResultText(update)).toBe("line1\nline2");
    });

    it("falls back to a string rawOutput", () => {
      expect(toolResultText({ rawOutput: "done" })).toBe("done");
    });

    it("pretty-prints a non-string rawOutput", () => {
      expect(toolResultText({ rawOutput: { ok: true } })).toBe(JSON.stringify({ ok: true }, null, 2));
    });

    it("returns empty string when there is nothing to show", () => {
      expect(toolResultText({})).toBe("");
    });
  });

  describe("connToChatStatus", () => {
    it("maps idle and ready to ready", () => {
      expect(connToChatStatus("idle")).toBe("ready");
      expect(connToChatStatus("ready")).toBe("ready");
    });

    it("maps running to working and disconnected to error", () => {
      expect(connToChatStatus("running")).toBe("working");
      expect(connToChatStatus("disconnected")).toBe("error");
    });

    it("maps in-flight handshake states to thinking", () => {
      expect(connToChatStatus("connecting")).toBe("thinking");
      expect(connToChatStatus("initializing")).toBe("thinking");
      expect(connToChatStatus("creating-session")).toBe("thinking");
      expect(connToChatStatus("loading-session")).toBe("thinking");
    });
  });
});
