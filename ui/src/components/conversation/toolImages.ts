export type ToolImage = { mimeType: string; data: string };

// Raster only: SVG can carry script, so it and non-images are dropped.
const RENDERABLE_IMAGE = /^image\/(png|jpeg|gif|webp)$/i;

/**
 * Splits images out of a tool response, leaving the rest to print as text. The
 * Go runtime sends `images: [{mimeType, data}]`; the Python runtime sends MCP
 * `{type: "image", data, mimeType}` items in `content`.
 */
export function splitToolImages(response: Record<string, unknown>): {
  images: ToolImage[];
  rest: Record<string, unknown>;
} {
  const images: ToolImage[] = [];
  const rest = { ...response };

  // Only a list wholly of images is taken; a tool's own `images` field or the Go note stays in the text.
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

