import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { MermaidDiagram } from "./MermaidDiagram";

/**
 * Mermaid is mocked because it cannot render in jsdom: its layout pass calls
 * `getBBox`, which jsdom does not implement. The unit test is about what this
 * component does with the renderer's result — the renderer itself is exercised
 * by the browser suite, where a real SVG is produced.
 */
const renderMermaid = vi.hoisted(() => vi.fn());

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: renderMermaid,
  },
}));

const SOURCE = "flowchart TD\nA-->B";

function renderDiagram(source: string) {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <MermaidDiagram source={source} />
    </ThemeProvider>,
  );
}

/** The raw source, as it appears in the fallback code frame. */
function fallbackCode() {
  return document.querySelector("pre code")?.textContent ?? null;
}

describe("MermaidDiagram", () => {
  it("shows the raw source while the diagram is being rendered", () => {
    renderMermaid.mockReturnValue(new Promise(() => {}));
    renderDiagram(SOURCE);

    expect(fallbackCode()).toBe(SOURCE);
    expect(screen.queryByTestId("chat-mermaid")).toBeNull();
  });

  it("renders the SVG mermaid produces", async () => {
    renderMermaid.mockResolvedValue({
      svg: '<svg data-testid="rendered"><text>Container starts</text></svg>',
      diagramType: "flowchart",
    });
    renderDiagram(SOURCE);

    const diagram = await screen.findByTestId("chat-mermaid");
    expect(diagram.querySelector("svg")).toBeTruthy();
    expect(diagram.textContent).toContain("Container starts");
  });

  it("falls back to the raw source when mermaid cannot render the diagram", async () => {
    renderMermaid.mockRejectedValue(new Error("parse error"));
    renderDiagram(SOURCE);

    // The fallback is the source in a code frame, not a broken diagram.
    expect(await screen.findByText(/flowchart TD/)).toBeTruthy();
    expect(fallbackCode()).toBe(SOURCE);
    expect(screen.queryByTestId("chat-mermaid")).toBeNull();
  });
});
