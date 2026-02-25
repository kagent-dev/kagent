import { render, screen } from "@testing-library/react";
import { StatusIndicator } from "../StatusIndicator";

// Mock useSidebar
const mockSidebarState = jest.fn(() => "expanded");
jest.mock("@/components/ui/sidebar", () => ({
  useSidebar: () => ({ state: mockSidebarState() }),
}));

jest.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: React.PropsWithChildren) => (
    <div data-testid="tooltip">{children}</div>
  ),
  TooltipContent: ({ children }: React.PropsWithChildren) => (
    <div data-testid="tooltip-content">{children}</div>
  ),
  TooltipProvider: ({ children }: React.PropsWithChildren) => (
    <div>{children}</div>
  ),
  TooltipTrigger: ({
    children,
    asChild,
  }: React.PropsWithChildren<{ asChild?: boolean }>) => (
    <div data-testid="tooltip-trigger">{children}</div>
  ),
}));

describe("StatusIndicator", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockSidebarState.mockReturnValue("expanded");
  });

  it("renders green dot and text in expanded state", () => {
    render(<StatusIndicator />);
    expect(screen.getByText("All systems operational")).toBeInTheDocument();
    const dot = document.querySelector(".bg-green-500");
    expect(dot).toBeInTheDocument();
  });

  it("renders dot with tooltip in collapsed state", () => {
    mockSidebarState.mockReturnValue("collapsed");
    render(<StatusIndicator />);

    // Should show tooltip with the status text
    expect(screen.getByTestId("tooltip")).toBeInTheDocument();
    expect(screen.getByTestId("tooltip-content")).toHaveTextContent(
      "All systems operational"
    );

    // Green dot should still be present
    const dot = document.querySelector(".bg-green-500");
    expect(dot).toBeInTheDocument();

    // Should NOT have the expanded wrapper with text-muted-foreground
    const expandedWrapper = document.querySelector(".text-muted-foreground");
    expect(expandedWrapper).not.toBeInTheDocument();
  });

  it("green dot has aria-hidden attribute", () => {
    render(<StatusIndicator />);
    const dot = document.querySelector(".bg-green-500");
    expect(dot).toHaveAttribute("aria-hidden", "true");
  });

  it("has muted foreground text styling in expanded state", () => {
    const { container } = render(<StatusIndicator />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain("text-muted-foreground");
    expect(wrapper.className).toContain("text-xs");
  });
});
