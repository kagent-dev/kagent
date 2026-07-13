import React from "react";
import { render, screen, waitFor, act } from "@testing-library/react";
import {
  SidebarStatusProvider,
  useSidebarStatus,
} from "@/lib/sidebar-status-context";
import { getPlugins } from "@/app/actions/plugins";

jest.mock("@/app/actions/plugins", () => ({
  getPlugins: jest.fn(),
}));

const mockedGetPlugins = getPlugins as jest.Mock;

const plugin = {
  name: "kanban",
  namespace: "kagent",
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
  afterEach(() => {
    jest.clearAllMocks();
  });

  it("loads plugins via the getPlugins action and reports ok", async () => {
    mockedGetPlugins.mockResolvedValue({ data: [plugin], message: "OK" });

    render(
      <SidebarStatusProvider>
        <Probe />
      </SidebarStatusProvider>
    );

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("ok"));
    expect(screen.getByTestId("plugins").textContent).toBe("kanban");
    expect(mockedGetPlugins).toHaveBeenCalled();
  });

  it("reports plugins-failed when the action returns an error", async () => {
    mockedGetPlugins.mockResolvedValue({ error: "boom", message: "boom" });

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

  it("reports plugins-failed when the action rejects", async () => {
    mockedGetPlugins.mockRejectedValue(new Error("network down"));

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
    mockedGetPlugins
      .mockResolvedValueOnce({ error: "unavailable", message: "unavailable" })
      .mockResolvedValueOnce({ data: [plugin], message: "OK" });

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
    expect(mockedGetPlugins).toHaveBeenCalledTimes(2);
  });
});
