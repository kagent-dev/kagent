import { describe, expect, it } from "vitest";
import { MAX_TOTAL_BYTES, isAllowedFile, mediaTypeOf, stageFiles, textLinks } from "./attachments";

const sized = (name: string, type: string, size: number) =>
  ({ name, type, size }) as File;

describe("mediaTypeOf", () => {
  it.each([
    ["notes.md", "", "text/markdown"],
    ["plan.YAML", "", "application/yaml"],
    ["page.htm", "", "text/html"],
    ["data.csv", "text/csv; charset=utf-8", "text/csv"],
    ["blob", "", "application/octet-stream"],
    ["data.csv", "application/vnd.ms-excel", "text/csv"],
    ["photo.JPG", "image/jpeg", "image/jpeg"],
  ])("%s (%j) is %s", (name, type, expected) => {
    expect(mediaTypeOf({ name, type })).toBe(expected);
  });
});

describe("textLinks", () => {
  it.each([
    ["summarize https://example.com/a/report.pdf?sig=abc", ["https://example.com/a/report.pdf?sig=abc"]],
    ["see (https://example.com/a.docx).", ["https://example.com/a.docx"]],
    ["https://example.com/a.pdf and https://example.com/a.pdf", ["https://example.com/a.pdf"]],
    ["what is https://github.com/kagent-dev/kagent", []],
    ["https://example.com/index.html", []],
    ["ftp://example.com/a.pdf example.com/a.pdf", []],
    ["", []],
  ])("%j is %j", (text, expected) => {
    expect(textLinks(text)).toEqual(expected);
  });
});

describe("isAllowedFile", () => {
  it.each([
    ["photo.png", "image/png", true],
    ["photo.webp", "", true],
    ["photo.heic", "image/heic", false],
    ["scan.tiff", "image/tiff", false],
    ["logo.svg", "image/svg+xml", false],
    ["page.htm", "", true],
    ["report.pdf", "application/pdf", true],
    ["deck.pptx", "", true],
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
      sized("big.csv", "text/csv", MAX_TOTAL_BYTES + 1),
      sized("x.exe", "", 1),
      sized("c.txt", "text/plain", 1024),
    ]);
    expect(result.files.map((file) => file.name)).toEqual(["a.txt", "c.txt"]);
    expect(result.error).toBe(
      "b.txt would put this message over 10 MB. big.csv would put this message over 10 MB. x.exe is not a supported file type.",
    );
  });

  it("reports nothing when everything fits", () => {
    expect(stageFiles([], [sized("a.md", "", 10)]).error).toBeUndefined();
  });
});
