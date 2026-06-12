"use client";

import { useEffect, useMemo } from "react";
import { FilePart, FileWithBytes } from "@a2a-js/sdk";
import { Download, File as FileIcon } from "lucide-react";
import { isFileWithBytes } from "@/lib/messageHandlers";

interface FileAttachmentProps {
  part: FilePart;
}

/** Format a byte count into a short human-readable string (e.g. "1.2 MB"). */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let size = bytes / 1024;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`;
}

/** Decode a base64 string into a Blob with the given MIME type. */
function base64ToBlob(base64: string, mimeType: string): Blob {
  const binary = atob(base64);
  const len = binary.length;
  const buffer = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    buffer[i] = binary.charCodeAt(i);
  }
  return new Blob([buffer], { type: mimeType || "application/octet-stream" });
}

/**
 * Renders a single A2A file part: an inline thumbnail for images, or a
 * downloadable chip (icon + filename + size) for all other types.
 */
export default function FileAttachment({ part }: FileAttachmentProps) {
  const file = part.file;

  const fileWithBytes: FileWithBytes | null = isFileWithBytes(file) ? file : null;
  const name = file.name || "file";
  const mimeType = file.mimeType || "application/octet-stream";
  const isImage = mimeType.startsWith("image/");

  const { objectUrl, byteSize } = useMemo(() => {
    if (!fileWithBytes) return { objectUrl: null, byteSize: null };
    try {
      const blob = base64ToBlob(fileWithBytes.bytes, mimeType);
      return { objectUrl: URL.createObjectURL(blob), byteSize: blob.size };
    } catch {
      return { objectUrl: null, byteSize: null };
    }
  }, [fileWithBytes, mimeType]);

  useEffect(() => {
    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [objectUrl]);

  // URI-based files (no inline bytes): render a plain external link.
  if (!fileWithBytes) {
    const uri = "uri" in file ? file.uri : undefined;
    return (
      <a
        href={uri}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-2 rounded-md border bg-background px-3 py-2 text-sm hover:bg-accent"
      >
        <FileIcon className="h-4 w-4 shrink-0" aria-hidden />
        <span className="truncate max-w-[16rem]">{name}</span>
      </a>
    );
  }

  if (isImage && objectUrl) {
    return (
      <a href={objectUrl} target="_blank" rel="noopener noreferrer" className="block w-fit">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={objectUrl}
          alt={name}
          className="max-h-48 max-w-xs rounded-md border object-contain"
        />
      </a>
    );
  }

  return (
    <div className="inline-flex items-center gap-2 rounded-md border bg-background px-3 py-2 text-sm">
      <FileIcon className="h-4 w-4 shrink-0" aria-hidden />
      <div className="flex flex-col min-w-0">
        <span className="truncate max-w-[16rem] font-medium">{name}</span>
        {byteSize !== null && (
          <span className="text-xs text-muted-foreground">{formatFileSize(byteSize)}</span>
        )}
      </div>
      {objectUrl && (
        <a
          href={objectUrl}
          download={name}
          className="ml-2 rounded p-1 hover:bg-accent"
          aria-label={`Download ${name}`}
        >
          <Download className="h-4 w-4" aria-hidden />
        </a>
      )}
    </div>
  );
}
