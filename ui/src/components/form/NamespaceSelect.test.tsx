import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useNamespaces } from "@/api";
import { NamespaceSelect } from "./NamespaceSelect";

vi.mock("@/api", () => ({ useNamespaces: vi.fn() }));

const loaded = (names: string[]) =>
  ({ data: names.map((name) => ({ name })), isLoading: false, error: undefined }) as unknown as ReturnType<typeof useNamespaces>;

describe("NamespaceSelect", () => {
  beforeEach(() => vi.mocked(useNamespaces).mockReset());

  it("offers the namespaces kagent can use and reports the one chosen", () => {
    vi.mocked(useNamespaces).mockReturnValue(loaded(["kagent", "team-a"]));
    const onChange = vi.fn();
    render(<NamespaceSelect onChange={onChange} data-testid="ns" />);
    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(screen.getByTitle("team-a"));
    expect(onChange).toHaveBeenCalledWith("team-a", expect.anything());
  });

  it("shows the current value", () => {
    vi.mocked(useNamespaces).mockReturnValue(loaded(["kagent"]));
    render(<NamespaceSelect value="kagent" onChange={() => {}} />);
    expect(screen.getByTitle("kagent")).toBeInTheDocument();
  });

  it("takes a typed namespace and says why when the list could not be read", () => {
    vi.mocked(useNamespaces).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("denied"),
    } as unknown as ReturnType<typeof useNamespaces>);
    const onChange = vi.fn();
    render(<NamespaceSelect id="ns" aria-describedby="ns-error" onChange={onChange} />);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "team-b" } });
    expect(onChange).toHaveBeenCalledWith("team-b");
    expect(input).toHaveAttribute("id", "ns");
    expect(input).toHaveAccessibleDescription(/Could not list namespaces \(denied\)/);
  });

  it("passes its id and error wiring to the focusable input", () => {
    vi.mocked(useNamespaces).mockReturnValue(loaded(["kagent"]));
    render(<NamespaceSelect id="ns" aria-invalid aria-describedby="ns-error" onChange={() => {}} />);
    const input = screen.getByRole("combobox");
    expect(input).toHaveAttribute("id", "ns");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute("aria-describedby", "ns-error");
  });
});
