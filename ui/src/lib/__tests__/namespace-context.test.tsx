import React from "react";
import { render, screen, act, renderHook } from "@testing-library/react";
import { NamespaceProvider, useNamespace } from "@/lib/namespace-context";

function Probe() {
  const { namespace, setNamespace } = useNamespace();
  return (
    <div>
      <span data-testid="ns">{namespace || "(empty)"}</span>
      <button onClick={() => setNamespace("kagent")}>set</button>
    </div>
  );
}

describe("NamespaceProvider", () => {
  it("defaults to an empty namespace and updates via setNamespace", () => {
    render(
      <NamespaceProvider>
        <Probe />
      </NamespaceProvider>
    );

    expect(screen.getByTestId("ns").textContent).toBe("(empty)");

    act(() => {
      screen.getByText("set").click();
    });

    expect(screen.getByTestId("ns").textContent).toBe("kagent");
  });

  it("throws when used outside a provider", () => {
    const spy = jest.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useNamespace())).toThrow(
      "useNamespace must be used within a NamespaceProvider"
    );
    spy.mockRestore();
  });
});
