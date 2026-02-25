import { render, screen, within } from "@testing-library/react";

// Mock next/font/google
jest.mock("next/font/google", () => ({
  Geist: () => ({ className: "mock-geist" }),
}));

// Mock next/navigation
jest.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), prefetch: jest.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

// Mock ThemeProvider
jest.mock("@/components/ThemeProvider", () => ({
  ThemeProvider: ({ children }: React.PropsWithChildren) => <div data-testid="theme-provider">{children}</div>,
}));

// Mock AppInitializer
jest.mock("@/components/AppInitializer", () => ({
  AppInitializer: ({ children }: React.PropsWithChildren) => <div data-testid="app-initializer">{children}</div>,
}));

// Mock AgentsProvider
jest.mock("@/components/AgentsProvider", () => ({
  AgentsProvider: ({ children }: React.PropsWithChildren) => <>{children}</>,
}));

// Mock NamespaceProvider and useNamespace
jest.mock("@/lib/namespace-context", () => ({
  NamespaceProvider: ({ children }: React.PropsWithChildren) => <>{children}</>,
  useNamespace: () => ({ namespace: "default", setNamespace: jest.fn() }),
}));

// Mock TooltipProvider
jest.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: React.PropsWithChildren) => <>{children}</>,
}));

// Mock Toaster
jest.mock("@/components/ui/sonner", () => ({
  Toaster: () => <div data-testid="toaster" />,
}));

// Mock listNamespaces (used by NamespaceSelector inside AppSidebar)
jest.mock("@/app/actions/namespaces", () => ({
  listNamespaces: () => Promise.resolve({ data: [] }),
}));

// Mock sidebar primitives
jest.mock("@/components/ui/sidebar", () => {
  const React = require("react");
  return {
    useSidebar: () => ({ state: "expanded", open: true, setOpen: jest.fn(), openMobile: false, setOpenMobile: jest.fn(), isMobile: false, toggleSidebar: jest.fn() }),
    SidebarProvider: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <div data-testid="sidebar-provider" {...props}>{children}</div>
    ),
    SidebarInset: ({ children, className, ...props }: React.PropsWithChildren<{ className?: string }>) => (
      <div data-testid="sidebar-inset" className={className} {...props}>{children}</div>
    ),
    Sidebar: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
      <nav data-testid="sidebar" aria-label="Main navigation" {...props}>{children}</nav>
    ),
    SidebarHeader: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
    SidebarContent: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
    SidebarFooter: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
    SidebarRail: () => <div data-testid="sidebar-rail" />,
    SidebarGroup: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
    SidebarGroupLabel: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
    SidebarMenu: ({ children }: React.PropsWithChildren) => <ul>{children}</ul>,
    SidebarMenuItem: ({ children }: React.PropsWithChildren) => <li>{children}</li>,
    SidebarMenuButton: ({ children, isActive, asChild, ...props }: React.PropsWithChildren<{ isActive?: boolean; asChild?: boolean }>) => (
      <button data-active={isActive} {...props}>{children}</button>
    ),
  };
});

// Import RootLayout after mocks
import RootLayout from "../layout";

describe("Root layout (Step 3)", () => {
  it("renders SidebarProvider, AppSidebar, and SidebarInset with children", () => {
    // RootLayout returns <html><body>...</body></html>
    // React 19 treats html/body as singletons — they merge into document.
    // We query document.body directly.
    render(RootLayout({ children: <div data-testid="page-content">Hello</div> }) as React.ReactElement);

    // Query the document body (where React merges <body> content)
    expect(screen.getByTestId("sidebar-provider")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-inset")).toBeInTheDocument();
    expect(screen.getByTestId("page-content")).toBeInTheDocument();
  });

  it("renders children inside SidebarInset", () => {
    render(RootLayout({ children: <div data-testid="page-content">Hello</div> }) as React.ReactElement);

    const inset = screen.getByTestId("sidebar-inset");
    expect(within(inset).getByTestId("page-content")).toBeInTheDocument();
  });

  it("does NOT render Header or Footer", () => {
    render(RootLayout({ children: <div>content</div> }) as React.ReactElement);

    // Header and Footer should not be in the DOM at all
    expect(screen.queryByTestId("header")).not.toBeInTheDocument();
    expect(screen.queryByTestId("footer")).not.toBeInTheDocument();
  });

  it("AppSidebar is a sibling before SidebarInset inside SidebarProvider", () => {
    render(RootLayout({ children: <div>content</div> }) as React.ReactElement);

    const provider = screen.getByTestId("sidebar-provider");
    const sidebar = within(provider).getByTestId("sidebar");
    const inset = within(provider).getByTestId("sidebar-inset");
    expect(sidebar).toBeInTheDocument();
    expect(inset).toBeInTheDocument();
  });

  it("body has correct flex layout classes", () => {
    render(RootLayout({ children: <div>content</div> }) as React.ReactElement);

    // body should have flex (not flex-col) + h-screen + overflow-hidden
    expect(document.body.className).toContain("flex");
    expect(document.body.className).toContain("h-screen");
    expect(document.body.className).toContain("overflow-hidden");
    expect(document.body.className).not.toContain("flex-col");
  });
});
