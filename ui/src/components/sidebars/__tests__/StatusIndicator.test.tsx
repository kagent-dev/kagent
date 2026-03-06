import { render, screen, fireEvent } from "@testing-library/react";
import { StatusIndicator } from "../StatusIndicator";

const mockRetry = jest.fn();
const mockUseSidebarStatus = jest.fn(() => ({
  status: "ok" as const,
  retry: mockRetry,
}));

const mockSidebarState = jest.fn(() => "expanded");
jest.mock("@/components/ui/sidebar", () => ({
  useSidebar: () => ({ state: mockSidebarState() }),
}));

jest.mock("@/lib/sidebar-status-context", () => ({
  useSidebarStatus: () => mockUseSidebarStatus(),
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
    mockUseSidebarStatus.mockReturnValue({ status: "ok", retry: mockRetry });
  });

  it("renders All systems operational when status is ok", () => {
    render(<StatusIndicator />);
    expect(screen.getByText("All systems operational")).toBeInTheDocument();
    const dot = document.querySelector(".bg-green-500");
    expect(dot).toBeInTheDocument();
  });

  it("renders Plugins failed and Retry when status is plugins-failed", () => {
    mockUseSidebarStatus.mockReturnValue({
      status: "plugins-failed",
      retry: mockRetry,
    });
    render(<StatusIndicator />);
    expect(screen.getByText("Plugins failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
    const dot = document.querySelector(".bg-destructive");
    expect(dot).toBeInTheDocument();
  });

  it("renders Loading… when status is loading", () => {
    mockUseSidebarStatus.mockReturnValue({
      status: "loading",
      retry: mockRetry,
    });
    render(<StatusIndicator />);
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });

  it("calls retry when Retry button is clicked", () => {
    mockUseSidebarStatus.mockReturnValue({
      status: "plugins-failed",
      retry: mockRetry,
    });
    render(<StatusIndicator />);
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(mockRetry).toHaveBeenCalledTimes(1);
  });

  it("renders dot with tooltip in collapsed state", () => {
    mockSidebarState.mockReturnValue("collapsed");
    render(<StatusIndicator />);

    expect(screen.getByTestId("tooltip")).toBeInTheDocument();
    expect(screen.getByTestId("tooltip-content")).toHaveTextContent(
      "All systems operational"
    );

    const dot = document.querySelector(".bg-green-500");
    expect(dot).toBeInTheDocument();
  });

  it("green dot has aria-hidden attribute when ok", () => {
    render(<StatusIndicator />);
    const dot = document.querySelector(".bg-green-500");
    expect(dot).toHaveAttribute("aria-hidden", "true");
  });

  it("has muted foreground text styling in expanded state when ok", () => {
    const { container } = render(<StatusIndicator />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain("text-muted-foreground");
    expect(wrapper.className).toContain("text-xs");
  });
});
