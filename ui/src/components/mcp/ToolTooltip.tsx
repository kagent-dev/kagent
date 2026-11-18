import type { ReactElement } from "react";
import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";

/** Shows a tool's name and description on hover and keyboard focus of `children`. */
export function ToolTooltip({
  name,
  description,
  children,
}: {
  name: string;
  /** Undefined when the server has not reported the tool. */
  description?: string;
  children: ReactElement;
}) {
  const theme = useTheme();
  return (
    <Tooltip
      trigger={["hover", "focus"]}
      styles={{ root: { maxWidth: 320 } }}
      title={
        <div data-testid="tool-tooltip" css={{ display: "grid", gap: 2 }}>
          <span css={{ fontFamily: theme.font.mono, fontWeight: 600, wordBreak: "break-word" }}>{name}</span>
          <span css={{ opacity: description ? 1 : 0.75, overflowWrap: "anywhere" }}>
            {description || "No description reported."}
          </span>
        </div>
      }
    >
      {children}
    </Tooltip>
  );
}
