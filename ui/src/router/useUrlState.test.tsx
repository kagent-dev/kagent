import { act } from "react";
import { renderHook } from "@testing-library/react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it } from "vitest";
import {
  useUrlListState,
  useUrlListStates,
  useUrlNumberState,
  useUrlState,
  useUrlStateWriter,
} from "./useUrlState";

/**
 * What the address bar is allowed to hold, and what it must not.
 *
 * The property worth pinning is not "a value round-trips" — it is the two rules the
 * whole file exists for. **A default is absent**, so the plain page has a clean
 * address and "is anything active?" can be answered by looking at the URL. And **one
 * handler's worth of changes is one navigation**, because the alternative silently
 * loses whichever change ran first — which is precisely the shape of "clear
 * everything" and of "reset the page when the filter changes".
 */

function inRouter(initial: string) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>;
  };
}

/** The hook under test, plus the address it produced. */
function renderWithLocation<T>(use: () => T, initial = "/models") {
  return renderHook(() => ({ value: use(), search: useLocation().search }), {
    wrapper: inRouter(initial),
  });
}

describe("useUrlState", () => {
  it("reads a parameter the address already carries", () => {
    const { result } = renderWithLocation(() => useUrlState("q"), "/models?q=bedrock");

    expect(result.current.value[0]).toBe("bedrock");
  });

  it("falls back when the parameter is absent", () => {
    const { result } = renderWithLocation(() => useUrlState("view", "table"));

    expect(result.current.value[0]).toBe("table");
  });

  it("writes a chosen value into the address", () => {
    const { result } = renderWithLocation(() => useUrlState("q"));

    act(() => result.current.value[1]("ollama"));

    expect(result.current.search).toBe("?q=ollama");
  });

  it("removes the parameter when the value returns to its default", () => {
    // Otherwise every page anybody has ever typed in carries `?q=` forever, and a
    // link to "the unfiltered list" is indistinguishable from a link to a search for
    // nothing.
    const { result } = renderWithLocation(() => useUrlState("q"), "/models?q=ollama");

    act(() => result.current.value[1](""));

    expect(result.current.search).toBe("");
  });

  it("leaves parameters it does not own alone", () => {
    // A page owns `q`; the scenario switch, a contributed filter and anything else in
    // the address belong to somebody else and must survive a keystroke.
    const { result } = renderWithLocation(
      () => useUrlState("q"),
      "/models?mock=empty&q=old",
    );

    act(() => result.current.value[1]("new"));

    expect(new URLSearchParams(result.current.search).get("mock")).toBe("empty");
    expect(new URLSearchParams(result.current.search).get("q")).toBe("new");
  });
});

describe("useUrlListState", () => {
  it("reads no values as an empty selection, which callers read as 'all'", () => {
    const { result } = renderWithLocation(() => useUrlListState("ns"));

    expect(result.current.value[0]).toEqual([]);
  });

  it("reads a repeated parameter as every one of its values", () => {
    const { result } = renderWithLocation(
      () => useUrlListState("ns"),
      "/models?ns=kagent&ns=platform",
    );

    expect(result.current.value[0]).toEqual(["kagent", "platform"]);
  });

  it("writes each value as its own parameter, so nothing has to be escaped", () => {
    const { result } = renderWithLocation(() => useUrlListState("ns"));

    act(() => result.current.value[1](["kagent", "platform"]));

    expect(result.current.search).toBe("?ns=kagent&ns=platform");
  });

  it("removes the parameter entirely when the last value is deselected", () => {
    const { result } = renderWithLocation(
      () => useUrlListState("ns"),
      "/models?ns=kagent",
    );

    act(() => result.current.value[1]([]));

    expect(result.current.search).toBe("");
  });

  it("keeps the same array identity while the values are unchanged", () => {
    // A page uses this as a `useMemo` dependency. `getAll` builds a fresh array on
    // every call, so without this the row filter re-runs on every render — and the
    // memo that exists to avoid that would be doing nothing at all.
    const { result, rerender } = renderWithLocation(
      () => useUrlListState("ns"),
      "/models?ns=kagent",
    );
    const first = result.current.value[0];

    rerender();

    expect(result.current.value[0]).toBe(first);
  });
});

describe("useUrlNumberState", () => {
  it("reads a number the address carries", () => {
    const { result } = renderWithLocation(
      () => useUrlNumberState("page", 1),
      "/models?page=4",
    );

    expect(result.current.value[0]).toBe(4);
  });

  it("reads a value that is not a number as the fallback", () => {
    // The address bar is editable and links get mangled. `?page=banana` should show
    // the first page, not an empty table with nothing to explain it.
    const { result } = renderWithLocation(
      () => useUrlNumberState("page", 1),
      "/models?page=banana",
    );

    expect(result.current.value[0]).toBe(1);
  });

  it("omits the fallback from the address", () => {
    const { result } = renderWithLocation(
      () => useUrlNumberState("page", 1),
      "/models?page=4",
    );

    act(() => result.current.value[1](1));

    expect(result.current.search).toBe("");
  });
});

describe("useUrlListStates", () => {
  it("reads several filters at once", () => {
    const { result } = renderWithLocation(
      () => useUrlListStates(["ns", "provider"]),
      "/models?ns=kagent&provider=OpenAI&provider=Ollama",
    );

    expect(result.current.value).toEqual({
      ns: ["kagent"],
      provider: ["OpenAI", "Ollama"],
    });
  });

  it("keeps its identity while the address is unchanged", () => {
    const { result, rerender } = renderWithLocation(
      () => useUrlListStates(["ns"]),
      "/models?ns=kagent",
    );
    const first = result.current.value;

    rerender();

    expect(result.current.value).toBe(first);
  });
});

describe("useUrlStateWriter", () => {
  it("applies several changes in one navigation, losing none of them", () => {
    /*
     * The reason this hook exists. Two separate `setSearchParams` calls in one
     * handler both read the parameters as they were before either ran, so the second
     * overwrites the first — and the first is the one that gets lost. "Clear
     * everything" and "reset the page when the filter changes" are both exactly that
     * shape, so a page built on separate setters clears the search and leaves the
     * namespaces, or turns the page and forgets the sort.
     */
    const { result } = renderWithLocation(
      () => useUrlStateWriter(),
      "/models?q=old&ns=kagent&page=3",
    );

    act(() => result.current.value({ q: null, ns: null, page: null }));

    expect(result.current.search).toBe("");
  });

  it("composes two writes that land before React has re-rendered", async () => {
    /*
     * Found by a probe, not by reasoning about it. Clicking "clear filters" and then
     * typing into the search box in the next instant put the cleared namespace filter
     * straight back: the second write read the parameters as they were before the
     * first, because the router had not re-rendered in between. On screen that is a
     * filter that will not clear and a search reporting no matches for a row that is
     * plainly on the page.
     *
     * Both writes happen inside one `act`, which is what "before React re-renders"
     * means here.
     */
    const { result } = renderWithLocation(
      () => useUrlStateWriter(),
      "/models?q=old&ns=kagent",
    );

    await act(async () => {
      result.current.value({ q: null, ns: null });
      result.current.value({ q: "haiku" });
    });

    expect(result.current.search).toBe("?q=haiku");
  });

  it("can set and clear different parameters in the same call", () => {
    const { result } = renderWithLocation(
      () => useUrlStateWriter(),
      "/models?ns=kagent&page=3",
    );

    act(() => result.current.value({ ns: ["platform", "analytics"], page: null }));

    expect(result.current.search).toBe("?ns=platform&ns=analytics");
  });

  it("replaces rather than pushes, so typing does not bury the previous page", () => {
    // Six keystrokes are one intention. Pushed, they leave a reader pressing Back six
    // times to get off a page they arrived at once — and the first press appears to do
    // nothing, because it lands on the same list with one fewer letter in the box.
    //
    // Measured by going back rather than by counting entries, which `MemoryRouter`
    // does not expose: one step back from the list must reach the page before it.
    const { result } = renderHook(
      () => ({
        write: useUrlStateWriter(),
        navigate: useNavigate(),
        path: useLocation().pathname + useLocation().search,
      }),
      {
        wrapper: ({ children }: { children: React.ReactNode }) => (
          <MemoryRouter initialEntries={["/dashboard", "/models"]} initialIndex={1}>
            {children}
          </MemoryRouter>
        ),
      },
    );

    act(() => result.current.write({ q: "a" }));
    act(() => result.current.write({ q: "ab" }));
    expect(result.current.path).toBe("/models?q=ab");

    act(() => result.current.navigate(-1));

    expect(result.current.path).toBe("/dashboard");
  });
});
