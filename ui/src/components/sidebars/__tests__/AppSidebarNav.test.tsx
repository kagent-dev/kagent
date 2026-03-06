import { render, screen, fireEvent } from "@testing-library/react";
import { AppSidebarNav, NAV_SECTIONS } from "../AppSidebarNav";
import { waitFor } from "@testing-library/react";
import { act } from "react";

// Mock next/navigation
const mockPathname = jest.fn(() => "/agents");
jest.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
}));

jest.mock("next/link", () => {
  const React = require("react");
  return ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  );
});

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
      asChild: _asChild,
      "aria-current": ariaCurrent,
      ...props
    }: React.PropsWithChildren<{ isActive?: boolean; asChild?: boolean; "aria-current"?: string }>) => (
      <button data-active={isActive} data-testid="sidebar-menu-button" aria-current={ariaCurrent} {...props}>
        {children}
      </button>
    ),
    SidebarMenuBadge: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <span data-testid="sidebar-menu-badge" {...props}>{children}</span>
    ),
  };
});

describe("AppSidebarNav", () => {
  const mockFetch = jest.fn();

  beforeEach(() => {
    mockPathname.mockReturnValue("/agents");
    mockFetch.mockReturnValue(new Promise(() => {}));
    global.fetch = mockFetch as unknown as typeof fetch;
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

  it("renders 11 static nav items total", () => {
    render(<AppSidebarNav />);
    const items = screen.getAllByTestId("sidebar-menu-item");
    expect(items).toHaveLength(11);
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
      "Models",
      "Tools",
      "MCP Servers",
      "GIT Repos",
      "Organization",
      "Gateways",
    ]);
  });

  it("SidebarGroups have role='group' and aria-labelledby referencing section id", () => {
    render(<AppSidebarNav />);
    const groups = screen.getAllByTestId("sidebar-group");
    expect(groups).toHaveLength(4);

    groups.forEach((group) => {
      expect(group).toHaveAttribute("role", "group");
      expect(group).toHaveAttribute("aria-labelledby");
    });

    expect(groups[0]).toHaveAttribute("aria-labelledby", "nav-section-overview");
    expect(groups[1]).toHaveAttribute("aria-labelledby", "nav-section-agents");
    expect(groups[2]).toHaveAttribute("aria-labelledby", "nav-section-resources");
    expect(groups[3]).toHaveAttribute("aria-labelledby", "nav-section-admin");
  });

  it("SidebarGroupLabels have matching id attributes", () => {
    render(<AppSidebarNav />);
    const labels = screen.getAllByTestId("sidebar-group-label");

    expect(labels[0]).toHaveAttribute("id", "nav-section-overview");
    expect(labels[1]).toHaveAttribute("id", "nav-section-agents");
    expect(labels[2]).toHaveAttribute("id", "nav-section-resources");
    expect(labels[3]).toHaveAttribute("id", "nav-section-admin");
  });

  it("active item has aria-current='page'", () => {
    mockPathname.mockReturnValue("/agents");
    render(<AppSidebarNav />);
    const buttons = screen.getAllByTestId("sidebar-menu-button");
    const activeButton = buttons.find(
      (btn) => btn.getAttribute("aria-current") === "page"
    );
    expect(activeButton).toBeDefined();
    expect(activeButton).toHaveTextContent("My Agents");
  });

  it("non-active items do not have aria-current", () => {
    mockPathname.mockReturnValue("/agents");
    render(<AppSidebarNav />);
    const buttons = screen.getAllByTestId("sidebar-menu-button");
    const nonActiveButtons = buttons.filter(
      (btn) => btn.getAttribute("aria-current") !== "page"
    );
    // 11 total static items minus 1 active
    expect(nonActiveButtons).toHaveLength(10);
    nonActiveButtons.forEach((btn) => {
      expect(btn).not.toHaveAttribute("aria-current");
    });
  });

  it("renders Kanban Board dynamically in AGENTS from /api/plugins", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [
          {
            name: "kagent/kanban-mcp",
            pathPrefix: "kanban-mcp",
            displayName: "Kanban Board",
            icon: "kanban",
            section: "AGENTS",
          },
        ],
      }),
    });

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByText("Kanban Board")).toBeInTheDocument();
    });
  });

  it("renders PLUGINS section when plugin section is unknown", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [
          {
            name: "kagent/custom-mcp",
            pathPrefix: "custom",
            displayName: "Custom Plugin",
            icon: "puzzle",
            section: "CUSTOM",
          },
        ],
      }),
    });

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByText("PLUGINS")).toBeInTheDocument();
      expect(screen.getByText("Custom Plugin")).toBeInTheDocument();
    });
  });

  it("renders plugin badge when plugin posts kagent:plugin-badge event", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [
          {
            name: "kagent/kanban-mcp",
            pathPrefix: "kanban-mcp",
            displayName: "Kanban Board",
            icon: "kanban",
            section: "AGENTS",
          },
        ],
      }),
    });

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByText("Kanban Board")).toBeInTheDocument();
    });

    act(() => {
      window.dispatchEvent(
        new CustomEvent("kagent:plugin-badge", {
          detail: { plugin: "kanban-mcp", count: 7 },
        })
      );
    });

    await waitFor(() => {
      expect(screen.getByTestId("sidebar-menu-badge")).toHaveTextContent("7");
    });
  });

  it("shows loading indicator while plugins fetch is in-flight", () => {
    // Default mock returns a never-resolving promise
    render(<AppSidebarNav />);
    expect(screen.getByTestId("plugins-loading")).toBeInTheDocument();
    expect(screen.getByText("Loading plugins…")).toBeInTheDocument();
  });

  it("hides loading indicator after plugins load", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ data: [] }),
    });

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.queryByTestId("plugins-loading")).not.toBeInTheDocument();
    });
  });

  it("shows error state when plugins fetch fails", async () => {
    mockFetch.mockRejectedValue(new Error("Network error"));

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByTestId("plugins-error")).toBeInTheDocument();
      expect(screen.getByText("Plugins failed")).toBeInTheDocument();
      expect(screen.getByTestId("plugins-retry")).toBeInTheDocument();
    });
  });

  it("shows error state when plugins fetch returns non-ok status", async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    });

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByTestId("plugins-error")).toBeInTheDocument();
    });
  });

  it("retries plugins fetch when retry button is clicked", async () => {
    mockFetch.mockRejectedValueOnce(new Error("Network error"));

    render(<AppSidebarNav />);

    await waitFor(() => {
      expect(screen.getByTestId("plugins-error")).toBeInTheDocument();
    });

    // Now set up successful response for retry
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [
          {
            name: "kagent/kanban-mcp",
            pathPrefix: "kanban-mcp",
            displayName: "Kanban Board",
            icon: "kanban",
            section: "AGENTS",
          },
        ],
      }),
    });

    const callsBefore = mockFetch.mock.calls.length;
    fireEvent.click(screen.getByTestId("plugins-retry"));

    await waitFor(() => {
      expect(screen.queryByTestId("plugins-error")).not.toBeInTheDocument();
      expect(screen.getByText("Kanban Board")).toBeInTheDocument();
    });

    // Verify fetch was called again after retry
    expect(mockFetch.mock.calls.length).toBeGreaterThan(callsBefore);
  });
});
