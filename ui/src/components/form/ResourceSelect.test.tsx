import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ResourceSelect, SEARCH_MIN_OPTIONS, type ResourceSelectProps } from "./ResourceSelect";

function open(props: Partial<ResourceSelectProps>) {
  render(
    <ResourceSelect kind="harness" plural="harnesses" names={[]} loading={false} onChange={() => {}} data-testid="pick" {...props} />,
  );
  fireEvent.mouseDown(screen.getByRole("combobox"));
}

describe("ResourceSelect", () => {
  it("says it is loading while the list is read", () => {
    open({ loading: true });
    expect(screen.getByText("Loading harnesses…")).toBeInTheDocument();
  });

  it("shows the empty content when the list has nothing in it", () => {
    open({ empty: "No harnesses in this namespace." });
    expect(screen.getByText("No harnesses in this namespace.")).toBeInTheDocument();
  });

  it("marks the field invalid and says why when the read failed", () => {
    open({ error: "forbidden" });
    expect(screen.getByText("Could not load harnesses: forbidden")).toBeInTheDocument();
    expect(screen.getByTestId("pick")).toHaveClass("ant-select-status-error");
  });

  it("offers each name and reports the one chosen", () => {
    const onChange = vi.fn();
    open({ names: ["default", "gpu"], onChange });
    fireEvent.click(screen.getByTitle("gpu"));
    expect(onChange).toHaveBeenCalledWith("gpu", expect.anything());
  });

  it("puts the right article before the kind in the placeholder", () => {
    const { unmount } = render(
      <ResourceSelect kind="agent template" names={[]} loading={false} onChange={() => {}} />,
    );
    expect(screen.getByText("Choose an agent template")).toBeInTheDocument();
    unmount();
    render(<ResourceSelect kind="harness" names={[]} loading={false} onChange={() => {}} />);
    expect(screen.getByText("Choose a harness")).toBeInTheDocument();
  });

  it("offers type-to-search only once the list is long", () => {
    const names = (count: number) => Array.from({ length: count }, (_, at) => `ns-${at}`);
    const { unmount } = render(
      <ResourceSelect kind="namespace" names={names(SEARCH_MIN_OPTIONS)} loading={false} onChange={() => {}} data-testid="short" />,
    );
    expect(screen.getByTestId("short")).not.toHaveClass("ant-select-show-search");
    unmount();
    render(
      <ResourceSelect kind="namespace" names={names(SEARCH_MIN_OPTIONS + 1)} loading={false} onChange={() => {}} data-testid="long" />,
    );
    expect(screen.getByTestId("long")).toHaveClass("ant-select-show-search");
  });
});
