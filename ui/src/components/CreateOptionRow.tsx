import { useTheme } from "@emotion/react";
import { Typography } from "antd";
import { Link } from "react-router-dom";

/**
 * One row of the Agents page's Create modal. Exported through `@/appExtensions` so an
 * extension mounting at `app_agents_agentsList_createModal_options` looks like the
 * built-in rows instead of copying their styling.
 *
 * Must not import from `@/appExtensions`: that barrel re-exports this file, so doing so
 * would make an import cycle.
 */
export function CreateOptionRow({
  title,
  tagline,
  to,
  testId,
}: {
  title: string;
  tagline: string;
  to: string;
  testId?: string;
}) {
  const theme = useTheme();
  return (
    <Link
      to={to}
      data-testid={testId}
      css={{
        display: "block",
        padding: theme.space(3),
        borderRadius: theme.radius.md,
        border: `1px solid ${theme.color.border}`,
        "&:hover": { borderColor: `${theme.color.primary}A6`, background: theme.color.bgElevated },
        "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}` },
      }}
    >
      <div css={{ fontFamily: theme.font.mono, fontSize: 13, color: theme.color.text }}>{title}</div>
      <Typography.Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{tagline}</Typography.Text>
    </Link>
  );
}
