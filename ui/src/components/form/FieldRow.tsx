import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";

/**
 * Puts related fields side by side, such as name and namespace; they stack when the form is narrow.
 * Labels, hints and inputs line up across the row.
 */
export function FieldRow({
  children,
  columns,
}: {
  children: ReactNode;
  columns?: string;
}) {
  const theme = useTheme();
  return (
    <div
      css={{
        display: "grid",
        gridTemplateColumns: columns ?? "repeat(auto-fit, minmax(240px, 1fr))",
        ...(columns ? { "@media (max-width: 560px)": { gridTemplateColumns: "minmax(0, 1fr)" } } : {}),
        columnGap: theme.space(4),
        alignItems: "start",
        // Each field joins four shared rows (label, hint, control, error), so hints and inputs line up
        // across the row whatever the hint length or however many pills sit under a control.
        "& > .ant-form-item": {
          display: "grid",
          gridRow: "span 4",
          gridTemplateRows: "subgrid",
          alignItems: "start",
          minWidth: 0,
        },
        "& > .ant-form-item .ant-form-item-row, & > .ant-form-item .ant-form-item-control, & > .ant-form-item .ant-form-item-control-input, & > .ant-form-item .ant-form-item-control-input-content":
          { display: "contents" },
        // A switch keeps its own width.
        "& > .ant-form-item .ant-switch": { justifySelf: "start" },
      }}
    >
      {children}
    </div>
  );
}
