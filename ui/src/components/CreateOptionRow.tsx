import { useTheme } from "@emotion/react";
import { Typography } from "antd";
import { ChevronRight } from "lucide-react";
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
  description,
  to,
  testId,
}: {
  title: string;
  description: string;
  to: string;
  testId?: string;
}) {
  const theme = useTheme();
  return (
    <Link
      to={to}
      data-testid={testId}
      css={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: theme.space(2),
        padding: theme.space(3),
        borderRadius: theme.radius.md,
        border: `1px solid ${theme.color.border}`,
        "&:hover": { borderColor: `${theme.color.primary}A6`, background: theme.color.bgElevated },
        "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}` },
      }}
    >
      <div>
        <div css={{ fontFamily: theme.font.mono, fontSize: 13, color: theme.color.text }}>{title}</div>
        <Typography.Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{description}</Typography.Text>
      </div>
      {/* Says the row goes somewhere; decorative, since the row is already a link. */}
      <ChevronRight aria-hidden size={16} css={{ flexShrink: 0, color: theme.color.textMuted }} />
    </Link>
  );
}
