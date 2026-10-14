import { describe, expect, it } from "vitest";
import { MAX_TOTAL_BYTES, isAllowedFile, linkMediaType, mediaTypeOf, pastedLink, stageFiles } from "./attachments";

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

describe("linkMediaType", () => {
  it.each([
    ["https://example.com/photo.JPG?sig=abc#x", "image/jpeg"],
    ["https://arxiv.org/pdf/1512.03385", ""],
    ["https://example.com/logo.svg", ""],
  ])("%s is %j", (url, expected) => {
    expect(linkMediaType(url)).toBe(expected);
  });
});

describe("pastedLink", () => {
  it.each([
    ["  https://example.com/a.pdf\n", "https://example.com/a.pdf"],
    ["http://example.com", "http://example.com/"],
    ["see https://example.com/a.pdf", undefined],
    ["ftp://example.com/a.pdf", undefined],
    ["example.com/a.pdf", undefined],
    ["", undefined],
  ])("%j is %j", (text, expected) => {
    expect(pastedLink(text)).toBe(expected);
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
    ["report.pdf", "application/pdf", false],
    ["deck.pptx", "", false],
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
