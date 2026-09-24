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

  const trailingIcon = { flexShrink: 0, marginLeft: theme.space(2) } as const;
  const focusRing = {
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
        <Download
          size={14}
          aria-hidden
          className="attachment-download"
          css={{ ...trailingIcon, color: theme.color.textMuted, transition: "color 120ms" }}
        />
      ) : null}
    </>
  );

  const chip = {
    display: "inline-flex",
    alignItems: "center",
    gap: theme.space(2),
    maxWidth: 260,
    minHeight: 40,
    padding: `${theme.space(1)} ${theme.space(2)} ${theme.space(1)} ${theme.space(3)}`,
    border: `1px solid ${theme.color.border}`,
    borderRadius: theme.radius.sm,
    background: theme.color.bgElevated,
    color: theme.color.text,
    fontSize: 13,
    transition: "border-color 120ms, background 120ms, box-shadow 120ms, transform 80ms",
  } as const;

  if (canDownload) {
    return (
      <a
        data-testid="attachment-chip"
        ref={withBlobUrl}
        href={file.url}
        download={file.name}
        aria-label={`Download ${file.name}`}
        css={{
          ...chip,
          textDecoration: "none",
          cursor: "pointer",
          "&:hover": {
            color: theme.color.text,
            borderColor: theme.color.primaryText,
            background: theme.color.accentBg,
            "& .attachment-download": { color: theme.color.primaryText },
          },
          "&:active": {
            color: theme.color.text,
            borderColor: theme.color.primary,
            background: `color-mix(in srgb, ${theme.color.primary} 32%, ${theme.color.bgElevated})`,
            boxShadow: "inset 0 1px 3px rgba(0, 0, 0, 0.2)",
            transform: "translateY(1px)",
          },
          ...focusRing,
        }}
      >
        {body}
      </a>
    );
  }

  if (onRemove) {
    // The whole chip removes the file; the X is only the cue.
    return (
      <button
        type="button"
        data-testid="attachment-chip"
        aria-label={`Remove ${file.name}`}
        onClick={onRemove}
        css={{
          ...chip,
          font: "inherit",
          fontSize: chip.fontSize,
          textAlign: "left",
          cursor: "pointer",
          "&:hover": {
            borderColor: theme.color.dangerBorder,
            "& .attachment-remove": {
              background: theme.color.dangerBg,
              borderColor: theme.color.dangerBorder,
              color: theme.color.dangerText,
            },
          },
          "&:active": {
            borderColor: theme.color.danger,
            transform: "translateY(1px)",
            "& .attachment-remove": {
              background: theme.color.danger,
              borderColor: theme.color.danger,
              color: theme.color.textOnPrimary,
              transform: "scale(0.9)",
            },
          },
          ...focusRing,
        }}
      >
        {body}
        <span
          aria-hidden
          className="attachment-remove"
          css={{
            ...trailingIcon,
            display: "inline-grid",
            placeItems: "center",
            width: 22,
            height: 22,
            border: "1px solid transparent",
            borderRadius: theme.radius.sm - 4,
            color: theme.color.textMuted,
            transition: "background 120ms, color 120ms, transform 80ms",
          }}
        >
          <X size={14} />
        </span>
      </button>
    );
  }

  return (
    <span data-testid="attachment-chip" css={chip}>
      {body}
    </span>
  );
}
