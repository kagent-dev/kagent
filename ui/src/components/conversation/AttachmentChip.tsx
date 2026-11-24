import { useCallback, useRef, useState, type FocusEvent, type ReactElement } from "react";
import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import { Download, FileText, X } from "lucide-react";
import type { ConversationFilePart } from "@/api";
import { formatBytes } from "@/api/conversation/attachments";
import { cardStateStyles } from "@/components/agent/controlStyles";
import { halfway } from "@/theme/theme";

/** One file: staged in the composer (with `onRemove`), or sent in a message. */
export function AttachmentChip({
  file,
  onRemove,
}: {
  file: Omit<ConversationFilePart, "kind">;
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
  // Full name in a tooltip, on hover or focus, only when the chip cuts it off.
  const nameRef = useRef<HTMLSpanElement>(null);
  const [tipOpen, setTipOpen] = useState(false);
  const showTip = () => {
    const el = nameRef.current;
    setTipOpen(Boolean(el && el.scrollWidth > el.clientWidth));
  };
  const hideTip = () => setTipOpen(false);
  const tipHandlers = {
    onMouseEnter: showTip,
    // Keyboard focus only, so focus moved in code after a mouse removal stays quiet.
    onFocus: (event: FocusEvent<HTMLElement>) => {
      if (event.currentTarget.matches(":focus-visible")) showTip();
    },
    onMouseLeave: hideTip,
    onBlur: hideTip,
  };
  const withTooltip = (chipElement: ReactElement) => (
    <Tooltip title={file.name} open={tipOpen}>
      {chipElement}
    </Tooltip>
  );
  const hasBytes = Boolean(file.blob || file.url);
  const isImage = file.mediaType.startsWith("image/") && Boolean(file.blob);
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
          alt=""
          css={{ width: 28, height: 28, objectFit: "cover", borderRadius: theme.radius.sm - 4 }}
        />
      ) : (
        <FileText size={16} aria-hidden css={{ flexShrink: 0, color: theme.color.textMuted }} />
      )}
      <span css={{ display: "grid", minWidth: 0, lineHeight: 1.3 }}>
        <span
          ref={nameRef}
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
          css={{ ...trailingIcon, color: theme.color.textMuted }}
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
    padding: `${theme.space(2)} ${theme.space(2)} ${theme.space(2)} ${theme.space(3)}`,
    border: `1px solid ${theme.color.border}`,
    borderRadius: theme.radius.sm,
    background: theme.color.bgElevated,
    color: theme.color.text,
    fontSize: 13,
    transition: "none",
  } as const;
  const card = cardStateStyles(theme, theme.color.bgElevated);
  const downloadLit = { color: theme.color.primaryText };

  if (canDownload) {
    return withTooltip(
      <a
        {...tipHandlers}
        data-testid="attachment-chip"
        ref={withBlobUrl}
        href={file.url}
        download={file.name}
        {...(file.url && !file.blob ? { target: "_blank", rel: "noopener noreferrer" } : {})}
        aria-label={`Download ${file.name}`}
        css={{
          ...chip,
          textDecoration: "none",
          cursor: "pointer",
          "&:hover": { ...card["&:hover"], "& .attachment-download": downloadLit },
          "&:active": { ...card["&:active"], "& .attachment-download": downloadLit },
          ...focusRing,
        }}
      >
        {body}
      </a>,
    );
  }

  if (onRemove) {
    // The whole chip removes the file; the X is only the cue.
    return withTooltip(
      <button
        type="button"
        {...tipHandlers}
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
            background: halfway(theme.color.bgElevated, theme.color.dangerBg),
            "& .attachment-remove": { color: theme.color.dangerText },
          },
          "&:active": {
            borderColor: theme.color.dangerBorder,
            background: theme.color.dangerBg,
            "& .attachment-remove": { color: theme.color.dangerText },
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
            color: theme.color.textMuted,
          }}
        >
          <X size={14} />
        </span>
      </button>,
    );
  }

  return withTooltip(
    <span {...tipHandlers} data-testid="attachment-chip" css={chip}>
      {body}
    </span>,
  );
}
