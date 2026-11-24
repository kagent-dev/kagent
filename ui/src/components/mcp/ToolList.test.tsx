import { cleanup, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { ToolList } from "./ToolList";

function renderedNames(names: string[]) {
  render(
    <ThemeProvider theme={themeFor("light")}>
      <ToolList tools={names.map((name) => ({ name, description: "" }))} />
    </ThemeProvider>,
  );
  return screen.getAllByTestId("tool-list-item").map((item) => item.firstElementChild?.textContent);
}

function Search() {
  return <pre data-testid="search">{useLocation().search}</pre>;
}

describe("ToolList", () => {
  it("lists tools alphabetically, whatever order the server reported them in", () => {
    expect(renderedNames(["k8s_get_pods", "K8s_apply", "k8s_describe"])).toEqual(["K8s_apply", "k8s_describe", "k8s_get_pods"]);
  });

  it("orders numbered tools by number rather than character by character", () => {
    expect(renderedNames(["tool_10", "tool_2"])).toEqual(["tool_2", "tool_10"]);
  });

  it("gives names that differ only in case one fixed order", () => {
    const first = renderedNames(["get_pods", "Get_pods"]);
    cleanup();
    expect(renderedNames(["Get_pods", "get_pods"])).toEqual(first);
  });

  it("says when a tool has no description", () => {
    renderedNames(["quiet"]);
    expect(screen.getByTestId("tool-list-item").textContent).toContain("No description reported.");
  });

  it("links each name and reports which one was followed", async () => {
    const onSelect = vi.fn();
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <MemoryRouter>
          <ToolList
            tools={[{ name: "get_pods", description: "Lists pods." }]}
            toolTo={(tool) => `?q=${tool.name}`}
            onSelect={onSelect}
            highlight={["pods"]}
          />
          <Search />
        </MemoryRouter>
      </ThemeProvider>,
    );
    expect(screen.getAllByText("pods", { selector: "mark" })).toHaveLength(2);
    await userEvent.click(screen.getByTestId("tool-list-link"));
    expect(onSelect).toHaveBeenCalledWith("get_pods");
    expect(screen.getByTestId("search").textContent).toBe("?q=get_pods");
  });
});
