import React from "react";
import { render, screen, waitFor, act } from "@testing-library/react";
import {
  SidebarStatusProvider,
  useSidebarStatus,
} from "@/lib/sidebar-status-context";

const plugin = {
  name: "kanban",
  pathPrefix: "kanban",
  displayName: "Kanban",
  icon: "kanban",
  section: "Plugins",
};

function Probe() {
  const { status, plugins, retry } = useSidebarStatus();
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="plugins">{plugins.map((p) => p.name).join(",")}</span>
      <button onClick={retry}>retry</button>
    </div>
  );
}

describe("SidebarStatusProvider", () => {
  const originalFetch = global.fetch;

  afterEach(() => {
    global.fetch = originalFetch;
    jest.clearAllMocks();
  });

  it("loads plugins from /api/plugins and reports ok", async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: [plugin] }),
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    render(
      <SidebarStatusProvider>
        <Probe />
      </SidebarStatusProvider>
    );

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("ok"));
    expect(screen.getByTestId("plugins").textContent).toBe("kanban");
    expect(fetchMock).toHaveBeenCalledWith("/api/plugins");
  });

  it("reports plugins-failed when the request fails", async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValue({ ok: false, status: 500 }) as unknown as typeof fetch;

    render(
      <SidebarStatusProvider>
        <Probe />
      </SidebarStatusProvider>
    );

    await waitFor(() =>
      expect(screen.getByTestId("status").textContent).toBe("plugins-failed")
    );
    expect(screen.getByTestId("plugins").textContent).toBe("");
  });

  it("re-fetches when retry() is called", async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce({ ok: false, status: 503 })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ data: [plugin] }) });
    global.fetch = fetchMock as unknown as typeof fetch;

    render(
      <SidebarStatusProvider>
        <Probe />
      </SidebarStatusProvider>
    );

    await waitFor(() =>
      expect(screen.getByTestId("status").textContent).toBe("plugins-failed")
    );

    act(() => {
      screen.getByText("retry").click();
    });

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("ok"));
    expect(screen.getByTestId("plugins").textContent).toBe("kanban");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
