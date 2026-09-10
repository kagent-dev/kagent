import { render, screen } from "@testing-library/react"

import { Sidebar, SidebarProvider } from "@/components/ui/sidebar"

describe("Sidebar", () => {
  it("positions the desktop panel within its provider", () => {
    render(
      <SidebarProvider data-testid="sidebar-provider">
        <Sidebar data-testid="sidebar-panel">Sidebar content</Sidebar>
      </SidebarProvider>
    )

    expect(screen.getByTestId("sidebar-provider")).toHaveClass(
      "relative",
      "overflow-hidden",
    )
    expect(screen.getByTestId("sidebar-panel")).toHaveClass("absolute", "h-full")
    expect(screen.getByTestId("sidebar-panel")).not.toHaveClass("fixed", "h-svh")
  })
})
