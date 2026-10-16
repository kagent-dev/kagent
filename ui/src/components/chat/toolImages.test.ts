import { describe, expect, it } from "vitest";
import { splitToolImages } from "./toolImages";

describe("splitToolImages", () => {
  it("takes the Go runtime's images out from beside the output", () => {
    const { images, rest } = splitToolImages({
      output: "rendered",
      images: [{ mimeType: "image/png", data: "iVBORw0KGgo=" }],
    });

    expect(images).toEqual([{ mimeType: "image/png", data: "iVBORw0KGgo=" }]);
    expect(rest).toEqual({ output: "rendered" });
  });

  it("summarises the image items of a forwarded MCP CallToolResult", () => {
    const { images, rest } = splitToolImages({
      content: [
        { type: "text", text: "Panel rendered" },
        { type: "image", data: "R0lGODlh", mimeType: "image/gif" },
      ],
      isError: false,
    });

    expect(images).toEqual([{ mimeType: "image/gif", data: "R0lGODlh" }]);
    expect(rest).toEqual({
      content: [{ type: "text", text: "Panel rendered" }, "[image: image/gif]"],
      isError: false,
    });
  });

  it("renders no type that could carry script, and prints none of its data", () => {
    const { images, rest } = splitToolImages({
      images: [
        { mimeType: "image/svg+xml", data: "PHN2Zz4=" },
        { mimeType: "text/html", data: "PGh0bWw+" },
      ],
      content: [{ type: "image", data: "PHN2Zz4=", mimeType: "image/svg+xml" }],
    });

    expect(images).toEqual([]);
    expect(JSON.stringify(rest)).not.toContain("PHN2Zz4=");
    expect(JSON.stringify(rest)).not.toContain("PGh0bWw+");
  });

  it.each([
    ["a tool's own list of URLs", { images: ["https://example.com/a.png"], total: 3 }],
    [
      "a list mixing images with anything else",
      { images: [{ mimeType: "image/png", data: "iVBORw0KGgo=" }, "https://example.com/a.png"] },
    ],
    ["the Go runtime's note that the bytes were withheld", { output: "x", images: "1 image(s) returned for display; not shown" }],
    ["an empty list", { images: [] }],
  ])("keeps %s in the text, untouched", (_, response) => {
    expect(splitToolImages(response)).toEqual({ images: [], rest: response });
  });

  it("leaves a response without images as it was", () => {
    const response = { output: "3 pods running", content: [{ type: "text", text: "x" }] };

    expect(splitToolImages(response)).toEqual({ images: [], rest: response });
  });
});
