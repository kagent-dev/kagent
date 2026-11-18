import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { authorTone } from "./authorTone";

describe("authorTone", () => {
  const theme = themeFor("dark");

  it("gives one author the same colour every time", () => {
    expect(authorTone("critic_style", theme)).toEqual(authorTone("critic_style", theme));
  });

  it("tells the nodes of a workflow apart", () => {
    const authors = ["writer", "critic_clarity", "critic_accuracy", "critic_style", "writer_critics"];
    const tones = new Set(authors.map((author) => authorTone(author, theme).text));
    expect(tones.size).toBeGreaterThan(1);
  });

  it("draws from the theme's pill tokens and never the danger triple", () => {
    for (const author of ["writer", "critic_clarity", "classify", "a", ""]) {
      const tone = authorTone(author, theme);
      expect(tone.text).not.toBe(theme.color.dangerText);
      expect(Object.values(theme.color)).toContain(tone.background);
    }
  });
});
