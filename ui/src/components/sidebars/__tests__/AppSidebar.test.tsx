import { render, screen } from "@testing-library/react";
import { waitFor } from "@testing-library/react";
import { AppSidebar } from "../AppSidebar";

const mockFetch = jest.fn();

jest.mock("next/navigation", () => ({
  usePathname: () => "/",
}));

jest.mock("next/link", () => {
  const React = require("react");
  return ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  );
});

jest.mock("@/lib/namespace-context", () => ({
  useNamespace: () => ({ namespace: "default", setNamespace: jest.fn() }),
}));

jest.mock("@/components/ui/sidebar", () => {
  const React = require("react");
  return {
    Sidebar: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <div data-testid="sidebar" {...props}>{children}</div>
    ),
    SidebarHeader: ({ children }: React.PropsWithChildren) => (
      <header data-testid="sidebar-header">{children}</header>
    ),
    SidebarContent: ({ children }: React.PropsWithChildren) => (
      <div data-testid="sidebar-content">{children}</div>
    ),
    SidebarFooter: ({ children }: React.PropsWithChildren) => (
      <footer data-testid="sidebar-footer">{children}</footer>
    ),
    SidebarRail: () => <div data-testid="sidebar-rail" />,
    useSidebar: () => ({ state: "expanded" }),
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
      asChild: _asChild,
      ...props
    }: React.PropsWithChildren<{ isActive?: boolean; asChild?: boolean }>) => (
      <button data-active={isActive} data-testid="sidebar-menu-button" {...props}>
        {children}
      </button>
    ),
    SidebarMenuBadge: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <span data-testid="sidebar-menu-badge" {...props}>{children}</span>
    ),
  };
});

jest.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipContent: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipProvider: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipTrigger: ({
    children,
    asChild,
  }: React.PropsWithChildren<{ asChild?: boolean }>) => <div>{children}</div>,
}));

jest.mock("../NamespaceSelector", () => ({
  NamespaceSelector: () => <div data-testid="namespace-selector">Namespace</div>,
}));

jest.mock("@/components/ThemeToggle", () => ({
  ThemeToggle: () => <button type="button">Theme</button>,
}));

jest.mock("@/components/kagent-logo", () => ({
  __esModule: true,
  default: () => <span>Logo</span>,
}));

describe("AppSidebar", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    global.fetch = mockFetch;
  });

  it("shows All systems operational in the footer", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ data: [] }),
    });

    render(<AppSidebar />);

    await waitFor(() => {
      expect(screen.getByText("All systems operational")).toBeInTheDocument();
    });
  });

  it("shows single status Plugins failed in footer when plugins fetch fails", async () => {
    mockFetch.mockRejectedValue(new Error("Network error"));

    render(<AppSidebar />);

    await waitFor(() => {
      expect(screen.getByText("Plugins failed")).toBeInTheDocument();
    });

    expect(screen.queryByText("All systems operational")).not.toBeInTheDocument();
  });
});
