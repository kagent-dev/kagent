import { FilePart } from "@a2a-js/sdk";

// Per-file upload limit (10 MB), enforced client-side; the server guards too.
export const MAX_FILE_BYTES = 10 * 1024 * 1024;

// Allowlist of accepted MIME types (images handled separately via prefix).
// Rich documents (PDF, Office, EPUB, HTML) are extracted to text on the server
// so the model can read them. Kept in sync with the Go/Python runtimes — the
// common formats both tabula and markitdown support.
export const ALLOWED_MIME_TYPES = new Set<string>([
  "application/pdf",
  "text/plain",
  "text/markdown",
  "text/csv",
  "text/html",
  "application/json",
  "application/xml",
  "text/xml",
  "application/yaml",
  "application/x-yaml",
  "text/yaml",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "application/epub+zip",
]);

// File extensions allowed as a fallback when the browser reports an empty MIME.
export const ALLOWED_EXTENSIONS = [
  ".md",
  ".markdown",
  ".csv",
  ".json",
  ".xml",
  ".yaml",
  ".yml",
  ".txt",
  ".html",
  ".htm",
  ".pdf",
  ".docx",
  ".xlsx",
  ".pptx",
  ".epub",
];

// accept attribute mirroring the allowlist for the native file picker.
export const FILE_ACCEPT =
  "image/*,application/pdf,text/plain,text/markdown,text/csv,text/html,application/json," +
  "application/xml,text/xml,application/yaml,application/x-yaml,text/yaml," +
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document," +
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet," +
  "application/vnd.openxmlformats-officedocument.presentationml.presentation," +
  "application/epub+zip," +
  ".md,.markdown,.csv,.json,.xml,.yaml,.yml,.txt,.html,.htm,.pdf,.docx,.xlsx,.pptx,.epub";

/** Returns true if the file's type/extension is in the upload allowlist. */
export function isAllowedFile(file: File): boolean {
  const type = file.type;
  if (type.startsWith("image/")) return true;
  if (ALLOWED_MIME_TYPES.has(type)) return true;
  const lower = file.name.toLowerCase();
  return ALLOWED_EXTENSIONS.some((ext) => lower.endsWith(ext));
}

/** Validates a file against the allowlist and size limit. Returns an error message or null. */
export function validateFile(file: File): string | null {
  if (!isAllowedFile(file)) {
    return `"${file.name}" is not an allowed file type`;
  }
  if (file.size > MAX_FILE_BYTES) {
    return `"${file.name}" exceeds the 10 MB limit`;
  }
  return null;
}

/** Reads a File into a base64 A2A FilePart (inline bytes). */
export function fileToFilePart(file: File): Promise<FilePart> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      // result is a data URL: "data:<mime>;base64,<data>"
      const base64 = result.includes(",") ? result.split(",", 2)[1] : result;
      resolve({
        kind: "file",
        file: {
          name: file.name,
          mimeType: file.type || "application/octet-stream",
          bytes: base64,
        },
      });
    };
    reader.onerror = () => reject(reader.error ?? new Error("Failed to read file"));
    reader.readAsDataURL(file);
  });
}
