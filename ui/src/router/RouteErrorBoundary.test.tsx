import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { Outlet, RouterProvider } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AppExtensionsProvider } from "@/appExtensions";
import type { AppExtensionConfig } from "@/appExtensions";
import { appTheme } from "@/theme/theme";
import { createAppRouter } from "./router";

/**
 * The claim the boundary exists to make: a page that throws is replaced, and the
 * shell around it is not. Built from a fake extension so the assertion runs
 * against the real `createAppRouter` nesting rather than a copy of it.
 */
function Boom(): never {
  throw new Error("page exploded");
}

const crashing: AppExtensionConfig = {
  id: "crashing",
  name: "Crashing",
  routes: [
    {
      path: "/crash",
      element: <Boom />,
    },
  ],
  shell: {
    Layout: () => (
      <div>
        <nav>shell nav</nav>
        <Outlet />
      </div>
    ),
  },
};

describe("a page that throws", () => {
  it("is replaced by the boundary, with the shell still up", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    window.history.pushState({}, "", "/crash");

    render(
      <ThemeProvider theme={appTheme}>
        <AppExtensionsProvider extensions={[crashing]}>
          <RouterProvider router={createAppRouter([crashing])} />
        </AppExtensionsProvider>
      </ThemeProvider>,
    );

    expect(screen.getByTestId("route-error")).toBeVisible();
    expect(screen.getByText("shell nav")).toBeVisible();
  });
});
