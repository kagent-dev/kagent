import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppExtensionsProvider, ExtensionSlot } from "./index";
import type { AppExtensionConfig } from "./index";

/**
 * The behaviour a host page depends on when it mounts a slot: what appears when
 * an installed extension supplies a component, what appears when none does — the
 * case that matters more — and what appears when two of them do.
 */

function renderWithExtensions(
  extensions: readonly AppExtensionConfig[],
  ui: React.ReactNode,
) {
  return render(
    <AppExtensionsProvider extensions={extensions}>{ui}</AppExtensionsProvider>,
  );
}

const baseConfig: AppExtensionConfig = { id: "example", name: "Example" };

describe("ExtensionSlot with nothing configured", () => {
  it("renders nothing at all — no component, no wrapper element", () => {
    const { container } = renderWithExtensions(
      [baseConfig],
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
    expect(
      document.querySelector('[data-testid^="extension-slot-"]'),
    ).toBeNull();
  });

  it("stays empty for one point while another point is configured", () => {
    // The realistic case for a page rebuild: an extension is installed, but it
    // says nothing about this particular slot.
    const config: AppExtensionConfig = {
      ...baseConfig,
      slots: {
        app_shell_appLayout_appSidebar_footer: () => <span>footer</span>,
      },
    };

    const { container } = renderWithExtensions(
      [config],
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
  });

  it("is safe to mount with no provider above it", () => {
    // A page under unit test, or a story, has no AppExtensionsProvider. The
    // context defaults to the empty install rather than throwing.
    const { container } = render(
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(container).toBeEmptyDOMElement();
  });
});

describe("ExtensionSlot with a component configured", () => {
  it("renders the contribution, wrapped in a layout-neutral hook", () => {
    const config: AppExtensionConfig = {
      ...baseConfig,
      slots: {
        app_agents_agentsList_pageHeader_actions: () => (
          <button type="button">Run scan</button>
        ),
      },
    };

    renderWithExtensions(
      [config],
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(screen.getByRole("button", { name: "Run scan" })).toBeVisible();
    const wrapper = screen.getByTestId(
      "extension-slot-app_agents_agentsList_pageHeader_actions",
    );
    // `display: contents` so the wrapper never becomes a flex/grid item.
    expect(wrapper).toHaveStyle({ display: "contents" });
  });

  it("passes the point's context through to the component", () => {
    const config: AppExtensionConfig = {
      ...baseConfig,
      slots: {
        app_agents_agentsList_agentListItem_badge: ({
          agentName,
          namespace,
        }) => <span>{`${namespace}/${agentName}`}</span>,
      },
    };

    renderWithExtensions(
      [config],
      <ExtensionSlot
        id="app_agents_agentsList_agentListItem_badge"
        context={{ agentName: "planner", namespace: "kagent" }}
      />,
    );

    expect(screen.getByText("kagent/planner")).toBeVisible();
  });

  it("portals the overlay point out to the document body", () => {
    const config: AppExtensionConfig = {
      ...baseConfig,
      slots: {
        app_shell_appLayout_contentArea_globalOverlay: () => (
          <span>overlay</span>
        ),
      },
    };

    const { container } = renderWithExtensions(
      [config],
      <ExtensionSlot id="app_shell_appLayout_contentArea_globalOverlay" />,
    );

    // Rendered, but not inside the slot's own subtree — that is the whole
    // point of the portal.
    expect(screen.getByText("overlay")).toBeVisible();
    expect(container).toBeEmptyDOMElement();
    expect(
      screen.getByTestId(
        "extension-slot-app_shell_appLayout_contentArea_globalOverlay",
      ).parentElement,
    ).toBe(document.body);
  });
});

describe("ExtensionSlot with several extensions installed", () => {
  const first: AppExtensionConfig = {
    id: "first",
    name: "First",
    slots: {
      app_agents_agentsList_pageHeader_actions: () => (
        <button type="button">Run scan</button>
      ),
    },
  };
  const second: AppExtensionConfig = {
    id: "second",
    name: "Second",
    slots: {
      app_agents_agentsList_pageHeader_actions: () => (
        <button type="button">Export</button>
      ),
    },
  };

  /*
   * Both, rather than one winning. A point is a place in the layout, not a single
   * appointment: two installed extensions each contributing a header action is the
   * same situation as the page writing two buttons itself. Anything else would mean
   * installing a second extension silently switched off part of the first.
   */
  it("renders every contribution at the point, in install order", () => {
    renderWithExtensions(
      [first, second],
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    const wrapper = screen.getByTestId(
      "extension-slot-app_agents_agentsList_pageHeader_actions",
    );
    expect(
      [...wrapper.querySelectorAll("button")].map((button) => button.textContent),
    ).toEqual(["Run scan", "Export"]);
  });

  it("keeps them under the one wrapper the point declares", () => {
    renderWithExtensions(
      [first, second],
      <ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />,
    );

    expect(
      document.querySelectorAll('[data-testid^="extension-slot-"]'),
    ).toHaveLength(1);
  });

  it("gives each contribution the point's context", () => {
    const badge = (label: string) => ({
      app_agents_agentsList_agentListItem_badge: ({
        namespace,
      }: {
        namespace: string;
      }) => <span>{`${label}:${namespace}`}</span>,
    });

    renderWithExtensions(
      [
        { id: "a", name: "A", slots: badge("a") },
        { id: "b", name: "B", slots: badge("b") },
      ],
      <ExtensionSlot
        id="app_agents_agentsList_agentListItem_badge"
        context={{ agentName: "planner", namespace: "kagent" }}
      />,
    );

    expect(screen.getByText("a:kagent")).toBeVisible();
    expect(screen.getByText("b:kagent")).toBeVisible();
  });
});
