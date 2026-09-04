import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ToolCallCard } from "./ToolCallCard";

describe("ToolCallCard", () => {
  it("renders a denied legacy invocation as not run rather than failed", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_not_run",
            data: {
              name: "k8s_get_resources",
              response: { error: 'error tool "k8s_get_resources" call is rejected' },
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("not run")).toBeTruthy();
    expect(screen.getByText("Permission was denied, so this tool was not run.")).toBeTruthy();
    expect(screen.queryByText("failed")).toBeNull();
    expect(screen.queryByText(/call is rejected/)).toBeNull();
  });
});
