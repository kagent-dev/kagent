import { cleanup, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ToolServerTools } from "./ToolServerTools";

function renderedNames(names: string[]) {
  render(
    <ThemeProvider theme={themeFor("light")}>
      <ToolServerTools tools={names.map((name) => ({ name, description: "" }))} />
    </ThemeProvider>,
  );
  return screen.getAllByTestId("tool-server-tool").map((item) => item.textContent);
}

describe("ToolServerTools", () => {
  it("lists tools alphabetically, whatever order the server reported them in", () => {
    expect(
      renderedNames(["k8s_get_pods", "K8s_apply", "k8s_describe"]),
    ).toEqual(["K8s_apply", "k8s_describe", "k8s_get_pods"]);
  });

  it("orders numbered tools by number rather than character by character", () => {
    expect(renderedNames(["tool_10", "tool_2"])).toEqual([
      "tool_2",
      "tool_10",
    ]);
  });

  it("gives names that differ only in case one fixed order", () => {
    const first = renderedNames(["get_pods", "Get_pods"]);
    cleanup();
    expect(renderedNames(["Get_pods", "get_pods"])).toEqual(first);
  });
});
