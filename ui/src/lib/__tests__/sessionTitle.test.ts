import {
  deriveSessionTitle,
  isPlaceholderSessionTitle,
  PLACEHOLDER_SESSION_TITLE,
} from "@/lib/sessionTitle";

describe("sessionTitle", () => {
  describe("deriveSessionTitle", () => {
    it("truncates long messages", () => {
      expect(deriveSessionTitle("abcdefghijklmnopqrstuvwxyz")).toBe("abcdefghijklmnopqrst...");
    });

    it("returns short messages unchanged", () => {
      expect(deriveSessionTitle("Hello")).toBe("Hello");
    });

    it("returns empty for whitespace-only input", () => {
      expect(deriveSessionTitle("   ")).toBe("");
    });
  });

  describe("isPlaceholderSessionTitle", () => {
    it("treats empty and legacy Chat titles as placeholders", () => {
      expect(isPlaceholderSessionTitle(undefined)).toBe(true);
      expect(isPlaceholderSessionTitle("")).toBe(true);
      expect(isPlaceholderSessionTitle(PLACEHOLDER_SESSION_TITLE)).toBe(true);
      expect(isPlaceholderSessionTitle("chat")).toBe(true);
    });

    it("does not treat derived titles as placeholders", () => {
      expect(isPlaceholderSessionTitle("Summarize this doc")).toBe(false);
    });
  });
});
