import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ResourceSelect, type ResourceSelectProps } from "./ResourceSelect";

function open(props: Partial<ResourceSelectProps>) {
  render(
    <ResourceSelect kind="harness" plural="harnesses" names={[]} loading={false} onChange={() => {}} data-testid="pick" {...props} />,
  );
  fireEvent.mouseDown(screen.getByRole("combobox"));
}

describe("ResourceSelect", () => {
  it("keeps the browser's own suggestions off the search box", () => {
    open({});
    expect(screen.getByRole("combobox")).toHaveAttribute("autocomplete", "off");
  });

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
    expect(onChange).toHaveBeenCalledWith("gpu");
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

  it("filters the list as the user types", async () => {
    const onChange = vi.fn();
    render(<ResourceSelect kind="harness" names={["default", "gpu", "gpu-large"]} loading={false} onChange={onChange} data-testid="pick" />);
    expect(screen.getByTestId("pick")).toHaveClass("ant-select-show-search");
    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.keyboard("large");
    expect(screen.queryByTitle("default")).toBeNull();
    expect(screen.getByTitle("gpu-large")).toBeInTheDocument();
    await userEvent.click(screen.getByTitle("gpu-large"));
    expect(onChange).toHaveBeenCalledWith("gpu-large");
  });

  it("offers a typed name that is not in the list when allowed, and shows it once chosen", () => {
    function Picker() {
      const [value, setValue] = useState<string>();
      return (
        <ResourceSelect kind="harness" names={["default"]} loading={false} allowTyped value={value} onChange={setValue} data-testid="pick" />
      );
    }
    render(<Picker />);
    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "new-pool" } });
    fireEvent.click(screen.getByText('Use "new-pool"'));
    expect(screen.getByTestId("pick").querySelector(".ant-select-content")).toHaveTextContent(/^new-pool$/);
  });

  it("offers no typed name unless allowed", () => {
    open({ names: ["default", "gpu"] });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "other" } });
    expect(screen.queryByText('Use "other"')).toBeNull();
  });
});
