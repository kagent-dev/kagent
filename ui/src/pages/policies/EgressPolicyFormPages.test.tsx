import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { KAGENT_API_VERSION } from "@/api/grpc/wire";
import { themeFor } from "@/theme/theme";
import { EgressPolicyEditPage, EgressPolicyNewPage } from "./EgressPolicyFormPages";
import { FIX_FIELDS_MESSAGE, FIX_FIELDS_TITLE } from "@/components/form/useSubmitValidation";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  toastSaved: vi.fn(),
  policy: undefined as unknown,
}));

vi.mock("@/components/form/saveToast", () => ({ toastSaved: mocks.toastSaved }));
vi.mock("@/api", async (original) => ({
  ...(await original<typeof import("@/api")>()),
  apiClient: { egressPolicies: { create: mocks.create, update: mocks.update } },
  useNamespaces: () => ({ data: [{ name: "kagent" }], isLoading: false, error: undefined }),
  useInvalidateEgressPolicies: () => async () => {},
  useEgressPolicy: () => ({ data: mocks.policy, error: undefined, refresh: async () => {} }),
}));

const existing = {
  ref: "kagent/github",
  namespace: "kagent",
  name: "github",
  resource: {
    apiVersion: KAGENT_API_VERSION,
    kind: "EgressPolicy",
    metadata: { name: "github", namespace: "kagent", resourceVersion: "7" },
    spec: { destinations: [{ origin: "https://api.github.com" }, { origin: "https://uploads.github.com" }] },
  },
};

function renderAt(entry: string) {
  const router = createMemoryRouter(
    [
      { path: "/policies", element: <p>Policies list</p> },
      { path: "/policies/egress/new", element: <EgressPolicyNewPage /> },
      { path: "/policies/egress/:namespace/:name", element: <p>Policy detail</p> },
      { path: "/policies/egress/:namespace/:name/edit", element: <EgressPolicyEditPage /> },
    ],
    { initialEntries: [entry] },
  );
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <RouterProvider router={router} />
    </ThemeProvider>,
  );
  return router;
}

const type = (testId: string, value: string) => fireEvent.change(screen.getByTestId(testId), { target: { value } });

beforeEach(() => {
  vi.clearAllMocks();
  mocks.policy = existing;
  mocks.create.mockImplementation(async ({ namespace, name }) => ({ namespace, name }));
  mocks.update.mockResolvedValue(existing);
});

describe("new egress policy page", () => {
  it("marks every field required or optional and links back to the list", () => {
    renderAt("/policies/egress/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Policies");
    fireEvent.click(screen.getByTestId("egress-destination-0-auth").querySelectorAll("input")[2]);
    const required = screen.getAllByTestId("field-required").length;
    const optional = screen.getAllByTestId("field-optional").length;
    // Name, namespace, origin, header, audiences are required; auth, prefix, lifetime are optional.
    expect([required, optional]).toEqual([5, 3]);
  });

  it("shows inline errors and a submit error, and sends nothing, on an invalid submit", () => {
    renderAt("/policies/egress/new");
    expect(screen.queryByTestId("egress-form-submit-error")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    expect(screen.getByText("Enter a name.")).toBeInTheDocument();
    expect(screen.getByText("Enter an origin.")).toBeInTheDocument();
    const error = screen.getByTestId("egress-form-submit-error");
    expect(error).toHaveTextContent(FIX_FIELDS_TITLE);
    expect(error).toHaveTextContent(FIX_FIELDS_MESSAGE);
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(
      error.compareDocumentPosition(screen.getByTestId("egress-form-submit")) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(mocks.create).not.toHaveBeenCalled();

    // The message animates out, so the field's invalid state is what is checked.
    expect(screen.getByTestId("egress-form-name")).toHaveClass("ant-input-status-error");
    type("egress-form-name", "github");
    expect(screen.getByTestId("egress-form-name")).not.toHaveClass("ant-input-status-error");
  });

  it("files credential problems under their own fields", () => {
    renderAt("/policies/egress/new");
    type("egress-form-name", "github");
    type("egress-destination-0-origin", "https://api.example.com");
    fireEvent.click(screen.getByTestId("egress-destination-0-auth").querySelectorAll("input")[1]);
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    expect(screen.getByText("Enter a header name.")).toBeInTheDocument();
    expect(screen.getByText("Enter the Secret name.")).toBeInTheDocument();
    expect(screen.getByText("Enter the key in the Secret.")).toBeInTheDocument();
    expect(screen.getByLabelText(/Key in secret/)).toBeInTheDocument();
  });

  it("creates the policy, shows the save toast and lands on the detail page", async () => {
    renderAt("/policies/egress/new");
    type("egress-form-name", "github");
    type("egress-destination-0-origin", "https://api.github.com");
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    expect(await screen.findByText("Policy detail")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: "kagent",
        name: "github",
        resource: expect.objectContaining({ spec: { destinations: [{ origin: "https://api.github.com" }] } }),
      }),
    );
    expect(mocks.toastSaved).toHaveBeenCalledWith("created", "egress policy", "github");
  });

  it("guards the form while the create is pending, then lands on the detail page", async () => {
    let finish!: (value?: unknown) => void;
    mocks.create.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const router = renderAt("/policies/egress/new");
    type("egress-form-name", "github");
    type("egress-destination-0-origin", "https://api.github.com");
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("egress-form-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/policies/egress/new");

    finish({ namespace: "kagent", name: "github" });
    expect(await screen.findByText("Policy detail")).toBeInTheDocument();
  });

  it("keeps the form and explains a failed create", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderAt("/policies/egress/new");
    type("egress-form-name", "github");
    type("egress-destination-0-origin", "https://api.github.com");
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    expect(await screen.findByTestId("egress-form-submit-error")).toHaveTextContent("boom");
    expect(mocks.toastSaved).not.toHaveBeenCalled();
  });

  it("goes back to the list on Cancel when nothing changed", async () => {
    renderAt("/policies/egress/new");
    fireEvent.click(screen.getByTestId("egress-form-cancel"));
    expect(await screen.findByText("Policies list")).toBeInTheDocument();
  });

  it("asks before leaving a dirty form and stays on Keep editing", async () => {
    const router = renderAt("/policies/egress/new");
    type("egress-form-name", "github");
    fireEvent.click(screen.getByTestId("egress-form-cancel"));
    expect(await screen.findByTestId("egress-form-discard-body")).toHaveTextContent("Leaving discards your edits.");
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/policies/egress/new");
    fireEvent.click(screen.getByTestId("egress-form-cancel"));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Policies list")).toBeInTheDocument();
  });

  it("adds and removes destinations in the row list, and adds audiences as pills", () => {
    renderAt("/policies/egress/new");
    const list = screen.getByTestId("egress-destinations");
    expect(within(list).getAllByRole("listitem").length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByRole("button", { name: "Remove destination 1" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("egress-destinations-add"));
    expect(screen.getByTestId("egress-destination-1")).toBeInTheDocument();
    expect(screen.getByTestId("egress-destination-1-origin")).toHaveFocus();
    type("egress-destination-1-origin", "https://two.example.com");
    fireEvent.click(screen.getByRole("button", { name: "Remove destination 1" }));
    expect(screen.queryByTestId("egress-destination-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("egress-destinations-add")).toHaveFocus();

    fireEvent.click(screen.getByTestId("egress-destination-0-auth").querySelectorAll("input")[2]);
    const audiences = screen.getByTestId("egress-destination-0-audiences");
    fireEvent.change(audiences, { target: { value: "tools, billing" } });
    fireEvent.keyDown(audiences, { key: "Enter", code: "Enter", keyCode: 13, charCode: 13 });
    const pills = screen.getByTestId("egress-destination-0-audiences-list");
    expect(within(pills).getByText("tools")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove tools" }));
    expect(within(pills).queryByText("tools")).not.toBeInTheDocument();
    expect(within(pills).getByText("billing")).toBeInTheDocument();
  });
});

describe("edit egress policy page", () => {
  it("opens on the saved policy with name and namespace locked", async () => {
    renderAt("/policies/egress/kagent/github/edit");
    expect(screen.getByTestId("egress-form-name")).toHaveAttribute("readonly");
    expect(screen.getByTestId("egress-destination-1-origin")).toHaveValue("https://uploads.github.com");
    expect(screen.getAllByText("Fixed once the policy exists.")).toHaveLength(2);
  });

  it("saves, shows the toast and returns to the detail page", async () => {
    renderAt("/policies/egress/kagent/github/edit");
    fireEvent.click(screen.getByRole("button", { name: "Remove destination 2" }));
    fireEvent.click(screen.getByTestId("egress-form-submit"));
    expect(await screen.findByText("Policy detail")).toBeInTheDocument();
    expect(mocks.update).toHaveBeenCalledWith(
      expect.objectContaining({
        resource: expect.objectContaining({
          metadata: expect.objectContaining({ resourceVersion: "7" }),
          spec: { destinations: [{ origin: "https://api.github.com" }] },
        }),
      }),
    );
    expect(mocks.toastSaved).toHaveBeenCalledWith("saved", "egress policy", "github");
  });

  it("guards unsaved edits", async () => {
    renderAt("/policies/egress/kagent/github/edit");
    type("egress-destination-0-origin", "https://changed.example.com");
    fireEvent.click(screen.getByTestId("egress-form-cancel"));
    await waitFor(() => expect(screen.getByTestId("egress-form-discard-body")).toBeInTheDocument());
  });
});
