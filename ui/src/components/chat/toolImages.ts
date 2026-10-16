export type ToolImage = { mimeType: string; data: string };

/*
 * Raster types only. These become `data:` URIs in an <img>, and SVG is a document
 * that can carry script — so it, and anything that is not an image at all, is
 * dropped rather than rendered.
 */
const RENDERABLE_IMAGE = /^image\/(png|jpeg|gif|webp)$/i;

/**
 * Splits the images out of a tool response, leaving the rest to print as text.
 *
 * Two shapes carry them: the Go runtime puts `images: [{mimeType, data}]` beside
 * `output`, and the Python runtime forwards the MCP `CallToolResult` whole, with
 * `{type: "image", data, mimeType}` items in `content`. Either way the base64 is
 * removed from `rest` — a panel image printed as text is a wall of noise — and an
 * image of a type we will not render is dropped from both.
 */
export function splitToolImages(response: Record<string, unknown>): {
  images: ToolImage[];
  rest: Record<string, unknown>;
} {
  const images: ToolImage[] = [];
  const rest = { ...response };

  // Only a list made wholly of images is ours to take: a tool's own `images` field
  // (URLs, say) or the Go runtime's note that the bytes were withheld stays in the
  // text where it can be read.
  if (Array.isArray(rest.images) && rest.images.length > 0 && rest.images.every(isImage)) {
    images.push(...rest.images);
    delete rest.images;
  }
  if (Array.isArray(rest.content)) {
    rest.content = rest.content.map((item: unknown) => {
      if ((item as { type?: unknown } | null)?.type !== "image") return item;
      if (isImage(item)) images.push({ mimeType: item.mimeType, data: item.data });
      return `[image: ${String((item as { mimeType?: unknown }).mimeType)}]`;
    });
  }

  return { images: images.filter((image) => RENDERABLE_IMAGE.test(image.mimeType)), rest };
}

function isImage(value: unknown): value is ToolImage {
  const candidate = value as Partial<ToolImage> | null;
  return typeof candidate?.mimeType === "string" && typeof candidate.data === "string";
}

