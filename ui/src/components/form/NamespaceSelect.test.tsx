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

  it("says why when namespaces could not be read", () => {
    vi.mocked(useNamespaces).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("denied"),
    } as unknown as ReturnType<typeof useNamespaces>);
    render(<NamespaceSelect onChange={() => {}} />);
    fireEvent.mouseDown(screen.getByRole("combobox"));
    expect(screen.getByText("Could not load namespaces: denied")).toBeInTheDocument();
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
