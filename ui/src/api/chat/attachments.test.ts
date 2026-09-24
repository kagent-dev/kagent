import { describe, expect, it } from "vitest";
import { MAX_TOTAL_BYTES, isAllowedFile, mediaTypeOf, stageFiles } from "./attachments";

const sized = (name: string, type: string, size: number) =>
  ({ name, type, size }) as File;

describe("mediaTypeOf", () => {
  it.each([
    ["notes.md", "", "text/markdown"],
    ["plan.YAML", "", "application/yaml"],
    ["report.pdf", "application/pdf", "application/pdf"],
    ["data.csv", "text/csv; charset=utf-8", "text/csv"],
    ["blob", "", "application/octet-stream"],
    ["data.csv", "application/vnd.ms-excel", "text/csv"],
    ["photo.JPG", "image/jpeg", "image/jpeg"],
  ])("%s (%j) is %s", (name, type, expected) => {
    expect(mediaTypeOf({ name, type })).toBe(expected);
  });
});

describe("isAllowedFile", () => {
  it.each([
    ["photo.png", "image/png", true],
    ["photo.webp", "", true],
    ["photo.heic", "image/heic", false],
    ["scan.tiff", "image/tiff", false],
    ["logo.svg", "image/svg+xml", false],
    ["deck.pptx", "", true],
    ["page.htm", "", true],
    ["book.epub", "application/epub+zip", true],
    ["setup.exe", "application/x-msdownload", false],
    ["archive.zip", "", false],
  ])("%s (%j) → %s", (name, type, expected) => {
    expect(isAllowedFile({ name, type })).toBe(expected);
  });
});

describe("stageFiles", () => {
  it("keeps what fits and says why the rest was left out", () => {
    const staged = [sized("a.txt", "text/plain", 6 * 1024 * 1024)];
    const result = stageFiles(staged, [
      sized("b.txt", "text/plain", 5 * 1024 * 1024),
      sized("big.pdf", "application/pdf", MAX_TOTAL_BYTES + 1),
      sized("x.exe", "", 1),
      sized("c.txt", "text/plain", 1024),
    ]);
    expect(result.files.map((file) => file.name)).toEqual(["a.txt", "c.txt"]);
    expect(result.error).toBe(
      "b.txt would put this message over 10 MB. big.pdf would put this message over 10 MB. x.exe is not a supported file type.",
    );
  });

  it("reports nothing when everything fits", () => {
    expect(stageFiles([], [sized("a.md", "", 10)]).error).toBeUndefined();
  });
});
