import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { describe, expect, it, vi } from "vitest";
import { DeleteResourceButton } from "./DeleteResourceButton";

describe.each(["modal", "popover"] as const)("%s deletion confirmation", (confirmation) => {
  it("deletes normally unless the user explicitly chooses to stop active turns", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <DeleteResourceButton kind="conversation" name="test" confirmation={confirmation}
          forceLabel="Stop active turns and delete" onDelete={onDelete} onDeleted={() => {}} />
      </ConfigProvider>,
    );

    fireEvent.click(screen.getByTestId("delete-test"));
    const force = await screen.findByRole("checkbox", { name: "Stop active turns and delete" });
    expect(force).not.toBeChecked();
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDelete).toHaveBeenLastCalledWith(false));
  });

  it("passes force only for the confirmation where it was selected", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <DeleteResourceButton kind="conversation" name="test" confirmation={confirmation}
          forceLabel="Stop active turns and delete" onDelete={onDelete} onDeleted={() => {}} />
      </ConfigProvider>,
    );

    fireEvent.click(screen.getByTestId("delete-test"));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Stop active turns and delete" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDelete).toHaveBeenLastCalledWith(true));
    await waitFor(() => expect(screen.queryByRole("checkbox")).not.toBeInTheDocument());

    fireEvent.click(screen.getByTestId("delete-test"));
    expect(await screen.findByRole("checkbox", { name: "Stop active turns and delete" })).not.toBeChecked();
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDelete).toHaveBeenLastCalledWith(false));
  });
});
