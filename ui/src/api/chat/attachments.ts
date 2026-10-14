// One cap for the whole message: the gateway's gRPC limit is 16 MiB, framing included.
export const MAX_TOTAL_BYTES = 10 * 1024 * 1024;

/** Media type by extension, for files the browser types wrongly or not at all. */
const TYPE_BY_EXTENSION: Record<string, string> = {
  md: "text/markdown",
  markdown: "text/markdown",
  csv: "text/csv",
  json: "application/json",
  xml: "application/xml",
  yaml: "application/yaml",
  yml: "application/yaml",
  txt: "text/plain",
  html: "text/html",
  htm: "text/html",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  // Read from bytes by Responses-format models only; other models get a "could not be read" note.
  pdf: "application/pdf",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};

const ALLOWED_TYPES = new Set([
  ...Object.values(TYPE_BY_EXTENSION),
  "text/xml",
  "application/x-yaml",
  "text/yaml",
  "text/x-yaml",
]);

/** For the file picker's `accept`, so it offers the same files this accepts. */
export const ACCEPTED_FILES = [
  ...ALLOWED_TYPES,
  ...Object.keys(TYPE_BY_EXTENSION).map((extension) => `.${extension}`),
].join(",");

function extensionOf(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot === -1 ? "" : name.slice(dot + 1).toLowerCase();
}

// The reported type when allowed, else the extension's: browsers report "" for .md,
// and Windows reports .csv as application/vnd.ms-excel.
export function mediaTypeOf(file: Pick<File, "name" | "type">): string {
  const reported = file.type.split(";", 1)[0]?.trim().toLowerCase() ?? "";
  if (ALLOWED_TYPES.has(reported)) return reported;
  return TYPE_BY_EXTENSION[extensionOf(file.name)] || reported || "application/octet-stream";
}

export function isAllowedFile(file: Pick<File, "name" | "type">): boolean {
  return ALLOWED_TYPES.has(mediaTypeOf(file));
}

/**
 * A link in the message text, sent beside the text as an A2A url part with no bytes and
 * no chip. A model that reads links fetches it; for any other model the runtime drops the
 * part, and the URL in the text stays for the agent's fetch tool.
 */
export class LinkFile extends File {
  constructor(readonly url: string) {
    super([], fileNameFromUrl(url), { type: linkMediaType(url) });
  }
}

/**
 * The links in `text` to send as url parts: http(s) URLs to a file of a known type,
 * as written in the text, so the runtime finds them there. A web page stays text only:
 * no provider is known to read one as a file.
 */
export function textLinks(text: string): string[] {
  // ponytail: whitespace split and trimmed punctuation, not a full URL grammar; a URL
  // that ends in ")" loses it. Use a linkify parser if users paste such links.
  const links = text
    .split(/\s+/)
    .map((token) => token.replace(/^[(<"'[]+/, "").replace(/[.,;:!?)>"'\]]+$/, ""))
    .filter((url) => isHttpUrl(url) && !["", "text/html"].includes(linkMediaType(url)));
  return [...new Set(links)];
}

export function isHttpUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * The media type sent with a link part, by the path's extension, since the browser
 * cannot read another origin's Content-Type. "" for no known extension.
 */
export function linkMediaType(url: string): string {
  return TYPE_BY_EXTENSION[extensionOf(fileNameFromUrl(url))] ?? "";
}

// Last path segment, so a query string or fragment never becomes the name.
export function fileNameFromUrl(url: string): string {
  try {
    return decodeURIComponent(new URL(url).pathname.split("/").pop() ?? "") || "file";
  } catch {
    return "file";
  }
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Number((bytes / 1024).toFixed(1))} KB`;
  return `${Number((bytes / (1024 * 1024)).toFixed(1))} MB`;
}

/** Adds `incoming` to what is staged, with why anything was left out. */
export function stageFiles(
  staged: readonly File[],
  incoming: readonly File[],
): { files: File[]; error?: string } {
  const files = [...staged];
  let total = files.reduce((sum, file) => sum + file.size, 0);
  const problems: string[] = [];

  for (const file of incoming) {
    if (!isAllowedFile(file)) {
      problems.push(`${file.name} is not a supported file type.`);
    } else if (total + file.size > MAX_TOTAL_BYTES) {
      problems.push(`${file.name} would put this message over ${formatBytes(MAX_TOTAL_BYTES)}.`);
    } else {
      files.push(file);
      total += file.size;
    }
  }
  return problems.length ? { files, error: problems.join(" ") } : { files };
}

/** The message a new conversation is created for, with its files. */
export interface FirstMessage {
  text: string;
  files: File[];
}

// Handed to the new conversation's page in memory, not router state, so Back,
// forward and reload never send it again.
const handedOff = new Map<string, FirstMessage>();

export function handOffFirstMessage(conversationId: string, message: FirstMessage): void {
  handedOff.set(conversationId, message);
}

export function takeFirstMessage(conversationId: string): FirstMessage | undefined {
  const message = handedOff.get(conversationId);
  handedOff.delete(conversationId);
  return message;
}
