import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";

/**
 * The fields of the option picked just above, such as an auth mode, on a nested panel.
 * Two fields to a row at most, so hints line up; give a field `gridColumn: "1 / -1"` for a row of its own.
 */
export function OptionPanel({
  label,
  children,
  "data-testid": testId,
}: {
  /** Names the group for screen readers, e.g. "Actor JWT settings". */
  label: string;
  children: ReactNode;
  "data-testid"?: string;
}) {
  const theme = useTheme();
  return (
    <div
      role="group"
      aria-label={label}
      data-testid={testId}
      css={{
        display: "grid",
        gridTemplateColumns: `repeat(auto-fill, minmax(max(220px, calc((100% - ${theme.space(4)}) / 2)), 1fr))`,
        columnGap: theme.space(4),
        padding: `${theme.space(3)} ${theme.space(3)} 0`,
        marginBottom: theme.space(4),
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bg,
      }}
    >
      {children}
    </div>
  );
}
