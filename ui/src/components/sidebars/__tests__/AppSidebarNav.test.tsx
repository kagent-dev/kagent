import { render, screen } from "@testing-library/react";
import { AppSidebarNav, NAV_SECTIONS } from "../AppSidebarNav";

// Mock next/navigation
const mockPathname = jest.fn(() => "/agents");
jest.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
}));

// Mock SidebarProvider context that sidebar primitives require
jest.mock("@/components/ui/sidebar", () => {
  const React = require("react");
  return {
    SidebarGroup: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <div data-testid="sidebar-group" {...props}>{children}</div>
    ),
    SidebarGroupLabel: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <div data-testid="sidebar-group-label" {...props}>{children}</div>
    ),
    SidebarMenu: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <ul data-testid="sidebar-menu" {...props}>{children}</ul>
    ),
    SidebarMenuItem: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <li data-testid="sidebar-menu-item" {...props}>{children}</li>
    ),
    SidebarMenuButton: ({
      children,
      isActive,
      asChild,
      ...props
    }: React.PropsWithChildren<{ isActive?: boolean; asChild?: boolean }>) => (
      <button data-active={isActive} data-testid="sidebar-menu-button" {...props}>
        {children}
      </button>
    ),
  };
});

describe("AppSidebarNav", () => {
  beforeEach(() => {
    mockPathname.mockReturnValue("/agents");
  });

  it("renders all 4 section labels", () => {
    render(<AppSidebarNav />);
    const labels = screen.getAllByTestId("sidebar-group-label");
    expect(labels).toHaveLength(4);
    expect(labels[0]).toHaveTextContent("OVERVIEW");
    expect(labels[1]).toHaveTextContent("AGENTS");
    expect(labels[2]).toHaveTextContent("RESOURCES");
    expect(labels[3]).toHaveTextContent("ADMIN");
  });

  it("renders 12 nav items total", () => {
    render(<AppSidebarNav />);
    const items = screen.getAllByTestId("sidebar-menu-item");
    expect(items).toHaveLength(12);
  });

  it("sets data-active='true' on item matching current pathname", () => {
    mockPathname.mockReturnValue("/agents");
    render(<AppSidebarNav />);
    const buttons = screen.getAllByTestId("sidebar-menu-button");
    const activeButton = buttons.find(
      (btn) => btn.getAttribute("data-active") === "true"
    );
    expect(activeButton).toBeDefined();
    expect(activeButton).toHaveTextContent("My Agents");
  });

  it("does not set data-active on non-matching items", () => {
    mockPathname.mockReturnValue("/agents");
    render(<AppSidebarNav />);
    const buttons = screen.getAllByTestId("sidebar-menu-button");
    const activeButtons = buttons.filter(
      (btn) => btn.getAttribute("data-active") === "true"
    );
    expect(activeButtons).toHaveLength(1);
  });

  it("activates Dashboard for root path", () => {
    mockPathname.mockReturnValue("/");
    render(<AppSidebarNav />);
    const buttons = screen.getAllByTestId("sidebar-menu-button");
    const activeButton = buttons.find(
      (btn) => btn.getAttribute("data-active") === "true"
    );
    expect(activeButton).toHaveTextContent("Dashboard");
  });

  it("NAV_SECTIONS contains the correct items", () => {
    const allItems = NAV_SECTIONS.flatMap((s) => s.items);
    const labels = allItems.map((i) => i.label);
    expect(labels).toEqual([
      "Dashboard",
      "Live Feed",
      "My Agents",
      "Workflows",
      "Cron Jobs",
      "Kanban",
      "Models",
      "Tools",
      "MCP Servers",
      "GIT Repos",
      "Organization",
      "Gateways",
    ]);
  });
});
