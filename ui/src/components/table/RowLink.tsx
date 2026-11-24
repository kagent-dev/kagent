import { useTheme } from "@emotion/react";
import { Link, type LinkProps } from "react-router-dom";
import { ROW_LINK_ATTR } from "./rowClick";

/**
 * The name cell of a row made clickable by `linkRow`: the link the whole row stands for.
 * Bold rather than underlined, since the row carries the hover and pressed states.
 */
export function RowLink({ mono = true, ...props }: LinkProps & { mono?: boolean }) {
  const theme = useTheme();
  return (
    <Link
      {...props}
      {...{ [ROW_LINK_ATTR]: "" }}
      css={{
        "&&, &&:hover, &&:active": { color: theme.color.text, textDecoration: "none" },
        fontWeight: 600,
        ...(mono ? { fontFamily: theme.font.mono } : {}),
        wordBreak: "break-word",
        borderRadius: 2,
        "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 2 },
      }}
    />
  );
}
