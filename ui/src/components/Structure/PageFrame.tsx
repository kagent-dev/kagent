import type { ReactNode } from "react";
import { Typography } from "antd";
import { useTheme } from "@emotion/react";
import type { LucideIcon } from "lucide-react";

const { Title, Paragraph } = Typography;

interface PageFrameProps {
  /**
   * The page's heading, when it needs one.
   *
   * Optional because a surface can be named by something already on screen: the
   * new-conversation page sits beside a rail that names the agent, so a title
   * repeating it pushed the composer down for nothing.
   */
  title?: string;
  /** The resource kind's icon, shown before the title (see `resourceIcons`). */
  icon?: LucideIcon;
  description?: string;
  /** Rendered at the top right of the frame — page-level actions. */
  actions?: ReactNode;
  children?: ReactNode;
}

/** Shared page chrome: heading, optional blurb, actions, then content. */
export function PageFrame({
  title,
  icon: Icon,
  description,
  actions,
  children,
}: PageFrameProps) {
  const theme = useTheme();

  /*
   * A page with nothing to put in its header gets no header, not an empty one.
   *
   * The block below is laid out unconditionally and carries a bottom margin, so a page
   * that supplies no title still paid for the heading, the margin under it, and the
   * line-height of a `<Title>` wrapped around nothing — a band of empty space above the
   * content, which is exactly what the agent surfaces showed once they started letting
   * the rail beside them do the naming.
   */
  const hasHeader = Boolean(title || description || actions);

  return (
    <div data-testid="page-frame">
      {hasHeader ? (
        <div
          css={{
            display: "flex",
            alignItems: "flex-start",
            justifyContent: "space-between",
            gap: theme.space(4),
            marginBottom: theme.space(6),
          }}
        >
          <div>
            {title ? (
              // `&&` outranks antd's typography margins, which would add space under the header.
              <Title
                level={2}
                css={{ "&&": { margin: 0 }, ...(Icon ? { display: "flex", alignItems: "center", gap: theme.space(2) } : {}) }}
                data-testid="page-title"
              >
                {Icon ? <Icon size={24} aria-hidden css={{ flexShrink: 0, color: theme.color.textMuted }} /> : null}
                {title}
              </Title>
            ) : null}
            {description ? (
              <Paragraph
                css={{ "&&": { margin: `${theme.space(2)} 0 0` }, color: theme.color.textMuted }}
              >
                {description}
              </Paragraph>
            ) : null}
          </div>
          {actions ? <div css={{ flexShrink: 0 }}>{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </div>
  );
}
