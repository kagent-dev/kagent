import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import { Link, type LinkProps } from "react-router-dom";

import { linkInk } from "./linkStyles";

interface ResourceLinkProps extends LinkProps {
  /** Monospace, for a `namespace/name` identifier. Size and other type come from the caller. */
  mono?: boolean;
  /** Says where the link goes, on hover and keyboard focus. */
  tooltip?: string;
}

/**
 * A reference to another resource: accent text, underlined on hover, with a focus ring.
 * Pass `state={backLinkState(...)}` so the page it opens can lead back here.
 */
export function ResourceLink({ mono, tooltip, children, ...props }: ResourceLinkProps) {
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
      }}
    >
      {children}
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
