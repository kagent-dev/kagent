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
  pdf: "application/pdf",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  epub: "application/epub+zip",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
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

// Files handed to a new conversation's page. In memory, not router state, which
// survives a reload and would send them twice.
const handedOff = new Map<string, File[]>();

export function handOffFiles(conversationId: string, files: File[]): void {
  if (files.length) handedOff.set(conversationId, files);
}

export function takeHandedOffFiles(conversationId: string): File[] {
  const files = handedOff.get(conversationId) ?? [];
  handedOff.delete(conversationId);
  return files;
}
