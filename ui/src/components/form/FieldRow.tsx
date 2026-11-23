import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";

/**
 * Puts related fields side by side, such as name and namespace; they stack when the form is narrow.
 * Hints of different lengths would push the inputs out of line, so the shorter hint grows to match.
 */
export function FieldRow({
  children,
  columns,
  alignInputs = true,
}: {
  children: ReactNode;
  columns?: string;
  /** Set false to leave each field at its own height, so a long hint pushes only its own input down. */
  alignInputs?: boolean;
}) {
  const theme = useTheme();
  if (!alignInputs) {
    return (
      <div
        css={{
          display: "grid",
          gridTemplateColumns: columns ?? "repeat(auto-fit, minmax(240px, 1fr))",
          columnGap: theme.space(4),
          alignItems: "start",
        }}
      >
        {children}
      </div>
    );
  }
  return (
    <div
      css={{
        display: "grid",
        gridTemplateColumns: columns ?? "repeat(auto-fit, minmax(240px, 1fr))",
        ...(columns ? { "@media (max-width: 560px)": { gridTemplateColumns: "minmax(0, 1fr)" } } : {}),
        columnGap: theme.space(4),
        alignItems: "stretch",
        "& > .ant-form-item": { display: "flex", flexDirection: "column" },
        "& > .ant-form-item > .ant-form-item-row": { flex: 1, flexWrap: "nowrap", flexDirection: "column" },
        "& > .ant-form-item .ant-form-item-control": { flex: 1, display: "flex", flexDirection: "column" },
        "& > .ant-form-item .ant-form-item-control-input": { flex: 1 },
        "& > .ant-form-item .ant-form-item-control-input, & > .ant-form-item .ant-form-item-control-input-content": {
          display: "flex",
          flexDirection: "column",
          alignItems: "stretch",
        },
        "& > .ant-form-item .ant-form-item-control-input-content": { width: "100%", minWidth: 0 },
        // Stretching must not widen a switch.
        "& > .ant-form-item .ant-switch": { alignSelf: "flex-start" },
        "& > .ant-form-item .ant-form-item-control-input-content .ant-form-item-extra": { flex: 1 },
      }}
    >
      {children}
    </div>
  );
}
