import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { FilterBar, type FilterDefinition } from "./FilterBar";
import { useListView } from "./useListView";

/**
 * The filter bar, driven the way a reader drives it.
 *
 * What is worth pinning here is the contract the pages depend on, not the markup:
 *
 * - **Selecting nothing means everything.** The state the page opens in shows no
 *   pills, and the page reads it as "do not narrow" rather than as "narrow to
 *   nothing" — the difference between the full list and an empty table.
 * - **A pill removes its own filter and nothing else.** With three active, clicking
 *   one has to leave the other two exactly as they were.
 * - **Clear filters clears the search too.** A term left in the box from ten minutes
 *   ago is the most easily forgotten filter on the page, so a control that cleared the
 *   pills and left it would still be hiding rows for a reason the reader thought they
 *   had dismissed.
 * - **All of it is in the address**, which is what makes the view linkable and lets it
 *   survive a reload.
 *
 * The `Select` popup is not driven here. It is antd's own listbox, and in a browser it
 * behaves in ways jsdom does not reproduce — rc-select renders a second, invisible
 * `role="listbox"` for screen readers, which `getByRole` resolves to happily and then
 * waits forever on. `playwright/tests/lists/list-filters.spec.ts` carries that note in
 * full, and is where choosing a namespace from the control is asserted, because that
 * is where it can be asserted honestly.
 */

const NAMESPACES: FilterDefinition = {
  id: "ns",
  label: "Namespace",
  allLabel: "All namespaces",
  options: [{ value: "kagent" }, { value: "platform" }, { value: "analytics" }],
};

const PROVIDERS: FilterDefinition = {
  id: "provider",
  label: "Provider",
  allLabel: "All providers",
  options: [{ value: "openai", label: "OpenAI" }, { value: "ollama", label: "Ollama" }],
};

const FILTER_IDS = ["ns", "provider"];

/** Renders the bar at a given address, and exposes the address it moves to. */
function renderBar(initialEntry = "/models") {
  const seen = { search: "", selectedNamespaces: [] as readonly string[] };

  function Harness() {
    const view = useListView(FILTER_IDS);
    seen.search = useLocation().search;
    seen.selectedNamespaces = view.selected("ns");

    return (
      <FilterBar
        testId="bar"
        view={view}
        search={{ label: "Search models", placeholder: "Search models" }}
        filters={[NAMESPACES, PROVIDERS]}
      />
    );
  }

  render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Harness />
      </MemoryRouter>
    </ThemeProvider>,
  );

  return seen;
}

describe("FilterBar", () => {
  it("shows no pills when nothing is chosen, and reports the selection as empty", () => {
    const seen = renderBar();

    expect(screen.queryByTestId("bar-pills")).toBeNull();
    // Empty rather than "every namespace": the page reads this as "do not narrow",
    // which is what makes the default view the whole list.
    expect(seen.selectedNamespaces).toEqual([]);
  });

  it("renders one pill per chosen value, naming the filter it came from", () => {
    renderBar("/models?ns=kagent&ns=platform");

    expect(screen.getByTestId("bar-pill-ns-kagent")).toHaveTextContent(
      "Namespace: kagent",
    );
    expect(screen.getByTestId("bar-pill-ns-platform")).toHaveTextContent(
      "Namespace: platform",
    );
  });

  it("labels a pill with the option's label rather than its value", () => {
    // The reader chose "OpenAI"; `openai` is how the row happens to store it.
    renderBar("/models?provider=openai");

    expect(screen.getByTestId("bar-pill-provider-openai")).toHaveTextContent(
      "Provider: OpenAI",
    );
  });

  it("shows the search term as a pill, because it is a filter people forget", () => {
    renderBar("/models?q=bedrock");

    expect(screen.getByTestId("bar-pill-search")).toHaveTextContent("Search: bedrock");
  });

  it("removes only its own filter when a pill is clicked", async () => {
    const seen = renderBar("/models?ns=kagent&ns=platform&provider=openai");

    await userEvent.click(screen.getByTestId("bar-pill-ns-kagent"));

    expect(seen.selectedNamespaces).toEqual(["platform"]);
    expect(new URLSearchParams(seen.search).getAll("ns")).toEqual(["platform"]);
    // The other filter is untouched: removing one choice is not a reset.
    expect(new URLSearchParams(seen.search).getAll("provider")).toEqual(["openai"]);
  });

  it("drops the parameter entirely when its last pill goes", async () => {
    const seen = renderBar("/models?ns=kagent");

    await userEvent.click(screen.getByTestId("bar-pill-ns-kagent"));

    // Not `?ns=`, which would read as "narrowed to nothing" — the address has to
    // become the address of the unfiltered list again.
    expect(seen.search).not.toContain("ns=");
    expect(screen.queryByTestId("bar-pills")).toBeNull();
  });

  it("offers the clear pill only while something is active", () => {
    renderBar();
    expect(screen.queryByTestId("bar-pill-clear")).toBeNull();
  });

  it("clears every filter and the search term in one go", async () => {
    const seen = renderBar("/models?q=haiku&ns=kagent&provider=openai&page=3");

    await userEvent.click(screen.getByTestId("bar-pill-clear"));

    // Nothing left narrowing the list, and no stranded page number pointing at a
    // page the unfiltered list may not have.
    expect(seen.search).toBe("");
    expect(screen.queryByTestId("bar-pills")).toBeNull();
  });

  it("puts a typed search term into the address, so the view can be linked to", async () => {
    const seen = renderBar();

    await userEvent.type(screen.getByTestId("bar-search"), "bedrock");

    expect(new URLSearchParams(seen.search).get("q")).toBe("bedrock");
  });

  it("returns to page one when a filter changes", async () => {
    // Narrowing a list while on page four otherwise leaves a reader looking at an
    // empty table that is not empty — the rows moved, and the page number did not.
    const seen = renderBar("/models?ns=kagent&ns=platform&page=4");

    await userEvent.click(screen.getByTestId("bar-pill-ns-kagent"));

    expect(new URLSearchParams(seen.search).get("page")).toBeNull();
  });

  it("leaves parameters belonging to something else alone", async () => {
    const seen = renderBar("/models?mock=empty&ns=kagent");

    await userEvent.click(screen.getByTestId("bar-pill-clear"));

    expect(new URLSearchParams(seen.search).get("mock")).toBe("empty");
  });
});
