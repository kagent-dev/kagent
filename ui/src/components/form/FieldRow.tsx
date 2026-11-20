import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";

/** Puts related fields side by side, such as name and namespace; they stack when the form is narrow. */
export function FieldRow({ children }: { children: ReactNode }) {
  const theme = useTheme();
  return (
    <div
      css={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))",
        columnGap: theme.space(4),
        alignItems: "start",
      }}
    >
      {children}
    </div>
  );
}
