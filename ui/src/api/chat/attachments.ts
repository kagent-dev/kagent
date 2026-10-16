import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import { unified } from "unified";
import { visit } from "unist-util-visit";

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
    super([], fileNameFromUrl(url));
  }
}

// The model reads the text as markdown, so links are found the same way: bare URLs,
// <autolinks> and [markdown](links), but none inside code.
const markdown = unified().use(remarkParse).use(remarkGfm);

/**
 * The links in `text` to send as url parts: http(s) URLs to a file of a known type,
 * as written in the text, so the runtime finds them there. A web page stays text only:
 * no provider is known to read one as a file. A link to a private host stays text too:
 * the provider fetches the link itself and cannot reach it.
 */
export function textLinks(text: string): string[] {
  const links = new Set<string>();
  visit(markdown.parse(text), "link", ({ url }) => {
    const type = TYPE_BY_EXTENSION[extensionOf(fileNameFromUrl(url))];
    if (text.includes(url) && isHttpUrl(url) && !isPrivateHost(url) && type && type !== "text/html") {
      links.add(url);
    }
  });
  return [...links];
}

export function isHttpUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

// An IP, dotless or LAN host the provider likely cannot fetch.
function isPrivateHost(url: string): boolean {
  const host = new URL(url).hostname.toLowerCase().replace(/\.$/, "");
  return /^[\d.]+$/.test(host) || !host.includes(".") || /\.(local|localdomain|internal|svc|lan|home\.arpa)$/.test(host);
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
