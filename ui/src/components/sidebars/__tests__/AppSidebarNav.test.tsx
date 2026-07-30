import React from "react";
import { render, screen } from "@testing-library/react";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AppSidebarNav, NAV_SECTIONS, type PluginNav } from "../AppSidebarNav";

jest.mock("next/navigation", () => ({
  usePathname: () => "/agents",
}));

jest.mock("@/lib/sidebar-status-context", () => ({
  useSidebarStatus: jest.fn(),
}));

jest.mock("@/contexts/SubstrateFeaturesContext", () => ({
  useSubstrateEnabled: () => false,
}));

import { useSidebarStatus } from "@/lib/sidebar-status-context";

const mockedUseSidebarStatus = useSidebarStatus as jest.Mock;

function renderNav(plugins: PluginNav[] = []) {
  mockedUseSidebarStatus.mockReturnValue({ plugins, status: "ok", retry: jest.fn() });
  return render(
    <SidebarProvider>
      <AppSidebarNav />
    </SidebarProvider>,
  );
}

describe("AppSidebarNav", () => {
  afterEach(() => jest.clearAllMocks());

  it("exposes the RESOURCES listing links including Plugins Catalog", () => {
    renderNav();

    for (const item of NAV_SECTIONS.find((s) => s.label === "RESOURCES")!.items) {
      expect(screen.getByRole("link", { name: item.label })).toHaveAttribute("href", item.href);
    }
    expect(screen.getByRole("link", { name: "MCP & tools" })).toHaveAttribute("href", "/mcp");
  });

  it("merges plugins into a known section and honors defaultPath", () => {
    renderNav([
      {
        name: "kanban",
        namespace: "kagent",
        pathPrefix: "kanban",
        displayName: "Kanban",
        icon: "puzzle",
        section: "RESOURCES",
        defaultPath: "/boards",
      },
    ]);

    expect(screen.getByRole("link", { name: "Kanban" })).toHaveAttribute(
      "href",
      "/plugins/kanban/boards",
    );
  });

  it("groups unknown sections under their declared label", () => {
    renderNav([
      {
        name: "admin-ui",
        namespace: "kagent",
        pathPrefix: "admin-ui",
        displayName: "Admin UI",
        icon: "puzzle",
        section: "ADMIN",
      },
    ]);

    expect(screen.getByText("ADMIN")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Admin UI" })).toHaveAttribute(
      "href",
      "/plugins/admin-ui",
    );
  });

  it("falls back to PLUGINS when section is empty", () => {
    renderNav([
      {
        name: "misc",
        namespace: "kagent",
        pathPrefix: "misc",
        displayName: "Misc Plugin",
        icon: "puzzle",
        section: "",
      },
    ]);

    expect(screen.getByText("PLUGINS")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Misc Plugin" })).toBeInTheDocument();
  });
});
