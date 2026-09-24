import { useCallback } from "react";
import { useTheme } from "@emotion/react";
import { Download, FileText, X } from "lucide-react";
import type { ChatFilePart } from "@/api";
import { formatBytes } from "@/api/chat/attachments";

/** One file: staged in the composer (with `onRemove`), or sent in a message. */
export function AttachmentChip({
  file,
  onRemove,
}: {
  file: Omit<ChatFilePart, "kind">;
  onRemove?: () => void;
}) {
  const theme = useTheme();
  // Object URL made per element and revoked when it goes, so re-reads don't leak blobs.
  const withBlobUrl = useCallback(
    (element: HTMLImageElement | HTMLAnchorElement | null) => {
      if (!element || !file.blob) return;
      const url = URL.createObjectURL(file.blob);
      if (element instanceof HTMLImageElement) element.src = url;
      else element.href = url;
      return () => URL.revokeObjectURL(url);
    },
    [file.blob],
  );
  const hasBytes = Boolean(file.blob || file.url);
  const isImage = file.mediaType.startsWith("image/") && hasBytes;
  const canDownload = hasBytes && !onRemove;

  const interactive = {
    "&:hover": { borderColor: theme.color.borderStrong, background: theme.color.accentBg },
    "&:active": { opacity: 0.8 },
    "&:focus-visible": { outline: `2px solid ${theme.color.primary}`, outlineOffset: 1 },
  } as const;

  const body = (
    <>
      {isImage ? (
        <img
          ref={withBlobUrl}
          src={file.url}
          alt=""
          css={{ width: 28, height: 28, objectFit: "cover", borderRadius: theme.radius.sm - 4 }}
        />
      ) : (
        <FileText size={16} aria-hidden css={{ flexShrink: 0, color: theme.color.textMuted }} />
      )}
      <span css={{ display: "grid", minWidth: 0, lineHeight: 1.3 }}>
        <span
          data-testid="attachment-name"
          css={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
        >
          {file.name}
        </span>
        {file.size !== undefined ? (
          <span css={{ fontSize: 11, color: theme.color.textMuted }}>
            {formatBytes(file.size)}
          </span>
        ) : null}
      </span>
      {canDownload ? (
        <Download size={14} aria-hidden css={{ flexShrink: 0, color: theme.color.textMuted }} />
      ) : null}
    </>
  );

  const chip = {
    display: "inline-flex",
    alignItems: "center",
    gap: theme.space(2),
    maxWidth: 240,
    minHeight: 40,
    padding: `${theme.space(1)} ${theme.space(2)}`,
    border: `1px solid ${theme.color.border}`,
    borderRadius: theme.radius.sm,
    background: theme.color.bgElevated,
    color: theme.color.text,
    fontSize: 13,
  } as const;

  if (canDownload) {
    return (
      <a
        data-testid="attachment-chip"
        ref={withBlobUrl}
        href={file.url}
        download={file.name}
        aria-label={`Download ${file.name}`}
        css={{ ...chip, textDecoration: "none", "&:hover, &:active": { color: theme.color.text }, ...interactive }}
      >
        {body}
      </a>
    );
  }

  return (
    <span data-testid="attachment-chip" css={chip}>
      {body}
      {onRemove ? (
        <button
          type="button"
          aria-label={`Remove ${file.name}`}
          onClick={onRemove}
          css={{
            display: "inline-grid",
            placeItems: "center",
            flexShrink: 0,
            width: 22,
            height: 22,
            padding: 0,
            border: "1px solid transparent",
            borderRadius: theme.radius.sm - 4,
            background: "transparent",
            color: theme.color.textMuted,
            cursor: "pointer",
            ...interactive,
            "&:hover": { background: theme.color.dangerBg, color: theme.color.dangerText },
          }}
        >
          <X size={14} aria-hidden />
        </button>
      ) : null}
    </span>
  );
}
