import { render, screen } from "@testing-library/react";
import { MobileTopBar } from "../MobileTopBar";

// Mock SidebarTrigger
jest.mock("@/components/ui/sidebar", () => {
  const React = require("react");
  return {
    SidebarTrigger: (props: Record<string, unknown>) => (
      <button data-testid="sidebar-trigger" {...props} />
    ),
  };
});

// Mock KAgentLogoWithText
jest.mock("../kagent-logo-text", () => {
  const React = require("react");
  return {
    __esModule: true,
    default: ({ className }: { className?: string }) => (
      <div data-testid="kagent-logo-text" className={className} />
    ),
  };
});

describe("MobileTopBar", () => {
  it("renders SidebarTrigger", () => {
    render(<MobileTopBar />);
    expect(screen.getByTestId("sidebar-trigger")).toBeInTheDocument();
  });

  it("renders KAgent logo", () => {
    render(<MobileTopBar />);
    expect(screen.getByTestId("kagent-logo-text")).toBeInTheDocument();
  });

  it("has lg:hidden class to hide on desktop", () => {
    const { container } = render(<MobileTopBar />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain("lg:hidden");
  });

  it("has border-b for bottom border", () => {
    const { container } = render(<MobileTopBar />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain("border-b");
  });

  it("passes h-5 className to logo", () => {
    render(<MobileTopBar />);
    const logo = screen.getByTestId("kagent-logo-text");
    expect(logo.className).toContain("h-5");
  });
});
