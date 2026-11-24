import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";

interface SectionHeaderProps {
  /** Heading id, for the section's `aria-labelledby`. */
  id: string;
  title: ReactNode;
  hint?: ReactNode;
  /** Controls shown right-aligned beside the title. */
  actions?: ReactNode;
}

/** A form section's heading: title and hint, actions on the right, a hairline below. */
export function SectionHeader({ id, title, hint, actions }: SectionHeaderProps) {
  const theme = useTheme();
  return (
    <div
      css={{
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        justifyContent: "space-between",
        gap: theme.space(3),
        paddingBottom: theme.space(3),
        marginBottom: theme.space(4),
        borderBottom: `1px solid ${theme.color.border}`,
      }}
    >
      <div css={{ flex: "1 1 280px", minWidth: 0 }}>
        <h3 id={id} css={{ margin: 0, fontSize: 16, fontWeight: 600, lineHeight: 1.4, color: theme.color.text }}>
          {title}
        </h3>
        {hint ? (
          <div css={{ marginTop: theme.space(1), color: theme.color.textMuted, fontSize: 13 }}>{hint}</div>
        ) : null}
      </div>
      {actions ? <div css={{ flexShrink: 0 }}>{actions}</div> : null}
    </div>
  );
}
