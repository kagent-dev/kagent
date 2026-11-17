import { fireEvent, render, screen } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { Link, RouterProvider, createMemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { UnsavedChangesModal } from "./UnsavedChangesModal";
import { useUnsavedChangesGuard } from "./useUnsavedChangesGuard";

function Form({ dirty }: { dirty: boolean }) {
  const blocker = useUnsavedChangesGuard(dirty);
  return (
    <>
      <Link to="/list">Leave</Link>
      <UnsavedChangesModal blocker={blocker} testIdPrefix="test">Edits are lost.</UnsavedChangesModal>
    </>
  );
}

function renderAt(dirty: boolean) {
  const router = createMemoryRouter(
    [
      { path: "/form", element: <Form dirty={dirty} /> },
      { path: "/list", element: <p>List page</p> },
    ],
    { initialEntries: ["/form"] },
  );
  render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <RouterProvider router={router} />
    </ConfigProvider>,
  );
  return router;
}

describe("useUnsavedChangesGuard", () => {
  it("lets a clean form leave without asking", async () => {
    renderAt(false);
    fireEvent.click(screen.getByText("Leave"));
    expect(await screen.findByText("List page")).toBeInTheDocument();
    expect(screen.queryByTestId("test-discard-body")).not.toBeInTheDocument();
  });

  it("blocks a dirty form and stays when the user keeps editing", async () => {
    const router = renderAt(true);
    fireEvent.click(screen.getByText("Leave"));
    expect(await screen.findByTestId("test-discard-body")).toHaveTextContent("Edits are lost.");
    expect(router.state.location.pathname).toBe("/form");

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/form");
  });

  it("continues the blocked navigation on Discard", async () => {
    renderAt(true);
    fireEvent.click(screen.getByText("Leave"));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(await screen.findByText("List page")).toBeInTheDocument();
  });

  it("asks the browser before unloading only when dirty", () => {
    const dirty = new Event("beforeunload", { cancelable: true });
    const { unmount } = render(<Unload dirty />);
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
    unmount();

    const clean = new Event("beforeunload", { cancelable: true });
    render(<Unload dirty={false} />);
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);
  });
});

function Unload({ dirty }: { dirty: boolean }) {
  const router = createMemoryRouter([{ path: "/", element: <Form dirty={dirty} /> }]);
  return <RouterProvider router={router} />;
}
