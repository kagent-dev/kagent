import { renderHook, act } from "@testing-library/react";
import { render, screen } from "@testing-library/react";
import { NamespaceProvider, useNamespace } from "../namespace-context";

describe("NamespaceProvider", () => {
  it("throws when useNamespace is called outside provider", () => {
    // Suppress console.error for expected error
    const spy = jest.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useNamespace())).toThrow(
      "useNamespace must be used within a NamespaceProvider"
    );
    spy.mockRestore();
  });

  it("provides default empty namespace", () => {
    const { result } = renderHook(() => useNamespace(), {
      wrapper: NamespaceProvider,
    });
    expect(result.current.namespace).toBe("");
  });

  it("updates namespace via setNamespace", () => {
    const { result } = renderHook(() => useNamespace(), {
      wrapper: NamespaceProvider,
    });
    act(() => {
      result.current.setNamespace("production");
    });
    expect(result.current.namespace).toBe("production");
  });

  it("renders children", () => {
    render(
      <NamespaceProvider>
        <div data-testid="child">Hello</div>
      </NamespaceProvider>
    );
    expect(screen.getByTestId("child")).toHaveTextContent("Hello");
  });
});
