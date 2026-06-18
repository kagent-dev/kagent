import React from "react";
import { render, screen, act } from "@testing-library/react";
import { useParams } from "next/navigation";
import PluginPage from "../page";

jest.mock("next/navigation", () => ({ useParams: jest.fn() }));
jest.mock("next-themes", () => ({ useTheme: () => ({ resolvedTheme: "light" }) }));
jest.mock("@/lib/namespace-context", () => ({
  useNamespace: () => ({ namespace: "kagent" }),
}));

const mockUseParams = useParams as jest.Mock;

function postMessageFrom(origin: string, type: string, payload: unknown) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { origin, data: { type, payload } }));
  });
}

describe("PluginPage host<->plugin messaging", () => {
  beforeEach(() => {
    mockUseParams.mockReturnValue({ name: "kanban", path: undefined });
  });

  it("honors same-origin plugin messages", async () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban") as HTMLIFrameElement;

    postMessageFrom(window.location.origin, "kagent:resize", { height: 444 });
    expect(iframe.style.height).toBe("444px");

    postMessageFrom(window.location.origin, "kagent:title", { title: "My Board" });
    expect(await screen.findByText("My Board")).toBeTruthy();
  });

  it("ignores messages from a foreign origin", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban") as HTMLIFrameElement;

    postMessageFrom("https://evil.example", "kagent:resize", { height: 999 });
    expect(iframe.style.height).not.toBe("999px");

    postMessageFrom("https://evil.example", "kagent:title", { title: "Hacked" });
    expect(screen.queryByText("Hacked")).toBeNull();
  });
});
