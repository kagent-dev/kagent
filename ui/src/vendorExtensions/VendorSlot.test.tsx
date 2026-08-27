import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { VendorExtensionProvider, VendorSlot } from "./index";
import type { VendorExtensionConfig } from "./index";

/**
 * The behaviour a host page depends on when it mounts a slot: what appears when
 * the config supplies a component, and — the case that matters more — what
 * appears when it does not.
 */

function renderWithConfig(config: VendorExtensionConfig, ui: React.ReactNode) {
  return render(
    <VendorExtensionProvider config={config}>{ui}</VendorExtensionProvider>,
  );
}

const baseConfig: VendorExtensionConfig = { id: "example", name: "Example" };

describe("VendorSlot with nothing configured", () => {
  it("renders nothing at all — no component, no wrapper element", () => {
    const { container } = renderWithConfig(
      baseConfig,
      <VendorSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
    expect(
      document.querySelector('[data-testid^="vendor-slot-"]'),
    ).toBeNull();
  });

  it("stays empty for one point while another point is configured", () => {
    // The realistic case for a page rebuild: an extension is installed, but it
    // says nothing about this particular slot.
    const config: VendorExtensionConfig = {
      ...baseConfig,
      slots: {
        app_shell_appLayout_appSidebar_footer: () => <span>footer</span>,
      },
    };

    const { container } = renderWithConfig(
      config,
      <VendorSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
  });

  it("is safe to mount with no provider above it", () => {
    // A page under unit test, or a story, has no VendorExtensionProvider. The
    // context defaults to the empty config rather than throwing.
    const { container } = render(
      <VendorSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
  });
});

describe("VendorSlot with a component configured", () => {
  it("renders the contribution, wrapped in a layout-neutral hook", () => {
    const config: VendorExtensionConfig = {
      ...baseConfig,
      slots: {
        app_agents_agentsList_pageHeader_actions: () => (
          <button type="button">Run scan</button>
        ),
      },
    };

    renderWithConfig(
      config,
      <VendorSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(screen.getByRole("button", { name: "Run scan" })).toBeVisible();
    const wrapper = screen.getByTestId(
      "vendor-slot-app_agents_agentsList_pageHeader_actions",
    );
    // `display: contents` so the wrapper never becomes a flex/grid item.
    expect(wrapper).toHaveStyle({ display: "contents" });
  });

  it("passes the point's context through to the component", () => {
    const config: VendorExtensionConfig = {
      ...baseConfig,
      slots: {
        app_agents_agentsList_agentListItem_badge: ({
          agentName,
          namespace,
        }) => <span>{`${namespace}/${agentName}`}</span>,
      },
    };

    renderWithConfig(
      config,
      <VendorSlot
        id="app_agents_agentsList_agentListItem_badge"
        context={{ agentName: "planner", namespace: "kagent" }}
      />,
    );

    expect(screen.getByText("kagent/planner")).toBeVisible();
  });

  it("portals the overlay point out to the document body", () => {
    const config: VendorExtensionConfig = {
      ...baseConfig,
      slots: {
        app_shell_appLayout_contentArea_globalOverlay: () => (
          <span>overlay</span>
        ),
      },
    };

    const { container } = renderWithConfig(
      config,
      <VendorSlot id="app_shell_appLayout_contentArea_globalOverlay" />,
    );

    // Rendered, but not inside the slot's own subtree — that is the whole
    // point of the portal.
    expect(screen.getByText("overlay")).toBeVisible();
    expect(container).toBeEmptyDOMElement();
    expect(
      screen.getByTestId(
        "vendor-slot-app_shell_appLayout_contentArea_globalOverlay",
      ).parentElement,
    ).toBe(document.body);
  });
});
