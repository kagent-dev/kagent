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
    ["read [the report](https://example.com/a.pdf) now", ["https://example.com/a.pdf"]],
    ["read <https://example.com/a.pdf> now", ["https://example.com/a.pdf"]],
    ["https://example.com/a.pdf?v=(2)", ["https://example.com/a.pdf?v=(2)"]],
    ["(see https://example.com/a.pdf?v=(2)).", ["https://example.com/a.pdf?v=(2)"]],
    ["run `curl https://example.com/a.pdf` here", []],
    // GFM links www. as http://www., which is not in the text as written.
    ["www.example.com/a.pdf", []],
    ["", []],
    // Private hosts: the provider cannot fetch them.
    ...[
      "http://localhost:8080/a.pdf",
      "http://LOCALHOST./a.pdf",
      "http://fileserver/a.pdf",
      "http://127.0.0.1/a.pdf",
      "http://2130706433/a.pdf",
      "http://10.0.0.1/a.pdf",
      "<http://[::1]/a.pdf>",
      "http://0.0.0.0:8000/a.pdf",
      "http://printer.local/a.pdf",
      "http://files.corp.internal/a.pdf",
      "http://files.default.svc/a.pdf",
      "http://files.default.svc.cluster.local/a.pdf",
      "http://fileserver.lan/a.pdf",
      "http://nas.home.arpa/a.pdf",
      "http://box.localdomain/a.pdf",
    ].map((url): [string, string[]] => [url, []]),
    // Public hosts that look close to private ones.
    ...[
      "https://lan.example.com/a.pdf",
      "https://local.example.com/a.pdf",
      "https://example.svc.com/a.pdf",
    ].map((url): [string, string[]] => [url, [url]]),
    // Any IP address stays text, public ones too. An IPv6 host has no dot.
    ...["http://11.0.0.1/a.pdf", "http://172.32.0.1/a.pdf", "<http://[2001:db8::1]/a.pdf>"].map(
      (url): [string, string[]] => [url, []],
    ),
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
