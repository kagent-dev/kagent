import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import type { LucideIcon } from "lucide-react";
import { Link, type LinkProps } from "react-router-dom";

import { linkInk } from "./linkStyles";

interface ResourceLinkProps extends LinkProps {
  /** Monospace, for a `namespace/name` identifier. Size and other type come from the caller. */
  mono?: boolean;
  /** Says where the link goes, on hover and keyboard focus. */
  tooltip?: string;
  /** A resource kind's icon, shown before the text (see `resourceIcons`). */
  icon?: LucideIcon;
}

/**
 * A reference to another resource: accent text, underlined on hover, with a focus ring.
 * Pass `state={backLinkState(...)}` so the page it opens can lead back here.
 */
export function ResourceLink({ mono, tooltip, icon: Icon, children, ...props }: ResourceLinkProps) {
  const theme = useTheme();
  const link = (
    <Link
      {...props}
      css={{
        // Doubled class, so antd's own `a` rules (a Tag's `color: inherit`) do not win.
        "&&": { ...linkInk(theme), "&:hover, &:active": { textDecoration: "underline" } },
        ...(mono ? { fontFamily: theme.font.mono } : {}),
        wordBreak: "break-word",
        borderRadius: 2,
        "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 2 },
        ...(Icon ? { display: "inline-flex", alignItems: "flex-start", gap: 4, minWidth: 0 } : {}),
      }}
    >
      {Icon ? (
        <>
          {/* One line tall, so the icon centres on the first line when the name wraps. */}
          <span css={{ display: "inline-flex", alignItems: "center", height: "1lh", flexShrink: 0 }}>
            <Icon size={14} aria-hidden />
          </span>
          <span css={{ minWidth: 0 }}>{children}</span>
        </>
      ) : (
        children
      )}
    </Link>
  );
  return tooltip ? (
    <Tooltip title={tooltip} trigger={["hover", "focus"]}>
      {link}
    </Tooltip>
  ) : (
    link
  );
}
