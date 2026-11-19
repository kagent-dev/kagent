import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { HarnessEditPage, HarnessNewPage } from "./HarnessNewPage";
import { FIX_FIELDS_MESSAGE } from "@/components/form/useSubmitValidation";

const IMAGE = `ghcr.io/example/runtime@sha256:${"a".repeat(64)}`;

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  success: vi.fn(),
  harnesses: [] as unknown[],
}));

vi.mock("react-hot-toast", () => ({ default: { success: mocks.success } }));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { agentBuildingBlocks: { createHarness: mocks.create, updateHarness: mocks.update } },
  useInvalidateHarnesses: () => () => Promise.resolve(),
  useHarnesses: () => ({ data: mocks.harnesses, isLoading: false, error: undefined }),
  useNamespaces: () => ({ data: [{ name: "kagent" }, { name: "team-a" }], isLoading: false, error: undefined }),
  useWorkerPools: () => ({ data: [{ name: "kagent-default" }, { name: "gpu" }], isLoading: false, error: undefined }),
}));

function harness(name: string, spec: Record<string, unknown>) {
  return {
    namespace: "kagent",
    name,
    runtime: Object.keys(spec)[0],
    resource: { metadata: { namespace: "kagent", name, resourceVersion: "7" }, spec },
  };
}

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      { path: "/agents", element: <p>Harness list</p> },
      { path: "/harnesses/new", element: <HarnessNewPage /> },
      { path: "/harnesses/:namespace/:name/edit", element: <HarnessEditPage /> },
    ],
    { initialEntries: [path] },
  );
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RouterProvider router={router} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return router;
}

// An input event, as typing sends: it also marks the field touched.
const type = (testId: string, value: string) =>
  fireEvent.input(screen.getByTestId(testId), { target: { value } });

beforeEach(() => {
  vi.clearAllMocks();
  mocks.harnesses = [];
});

describe("harness create form", () => {
  it("shows a back link and marks every field required or optional", () => {
    renderAt("/harnesses/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Harnesses");
    // Name, Namespace, Runtime adapter, Worker pool, Snapshot location.
    expect(screen.getAllByTestId("field-required")).toHaveLength(5);
    // Workload image, Command, Arguments.
    expect(screen.getAllByTestId("field-optional")).toHaveLength(3);
  });

  it("picks the namespace and worker pool from dropdowns, with defaults filled in", () => {
    renderAt("/harnesses/new");
    expect(screen.getByTestId("harness-namespace")).toHaveClass("ant-select");
    expect(screen.getByTestId("harness-namespace")).toHaveTextContent("kagent");
    expect(screen.getByTestId("harness-worker-pool")).toHaveClass("ant-select");
    expect(screen.getByTestId("harness-worker-pool")).toHaveTextContent("kagent-default");
  });

  it("flags a typed problem at once, an emptied field once left, and holds Create back", () => {
    renderAt("/harnesses/new");
    const submit = screen.getByTestId("harness-submit");
    expect(submit).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByTestId("harness-blocked")).toHaveTextContent(FIX_FIELDS_MESSAGE);

    type("harness-image", "ghcr.io/example/runtime:latest");
    expect(screen.getByText("The workload image must be pinned by sha256 digest.")).toBeInTheDocument();
    type("harness-snapshot", "");
    expect(screen.queryByText("A snapshot location is required.")).not.toBeInTheDocument();
    fireEvent.blur(screen.getByTestId("harness-snapshot"));
    expect(screen.getByText("A snapshot location is required.")).toBeInTheDocument();
    expect(screen.queryByText("A name is required.")).not.toBeInTheDocument();

    fireEvent.click(submit);
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("creates the harness, shows the toast and lands on the list", async () => {
    mocks.create.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/harnesses/new");
    type("harness-name", "made-here");
    fireEvent.click(screen.getByTestId("harness-submit"));

    expect(await screen.findByText("Harness list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "made-here",
      resource: {
        metadata: { namespace: "kagent", name: "made-here" },
        spec: {
          kagent: {},
          substrate: {
            workerPoolRef: { name: "kagent-default" },
            snapshotPolicy: { location: "s3://ate-snapshots/kagent/" },
          },
        },
      },
    });
    expect(mocks.success).toHaveBeenCalledWith("Created harness made-here");
  });

  it("accepts a dotted name, as the API does", async () => {
    mocks.create.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/harnesses/new");
    type("harness-name", "team.reviewer");
    fireEvent.click(screen.getByTestId("harness-submit"));

    expect(await screen.findByText("Harness list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: "team.reviewer" }));
  });

  it("takes a worker pool that is not in the list yet", async () => {
    mocks.create.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/harnesses/new");
    type("harness-name", "made-here");
    const pool = within(screen.getByTestId("harness-worker-pool")).getByRole("combobox");
    fireEvent.mouseDown(pool);
    fireEvent.change(pool, { target: { value: "new-pool" } });
    fireEvent.click(screen.getByText('Use "new-pool"'));
    expect(screen.getByTestId("harness-worker-pool").querySelector(".ant-select-content")).toHaveTextContent(/^new-pool$/);
    fireEvent.click(screen.getByTestId("harness-submit"));

    expect(await screen.findByText("Harness list")).toBeInTheDocument();
    const sent = mocks.create.mock.calls[0][0] as { resource: { spec: { substrate: { workerPoolRef: { name: string } } } } };
    expect(sent.resource.spec.substrate.workerPoolRef).toEqual({ name: "new-pool" });
  });

  it("explains a failed create above the buttons and keeps the form", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderAt("/harnesses/new");
    type("harness-name", "made-here");
    fireEvent.click(screen.getByTestId("harness-submit"));
    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(screen.getByTestId("harness-error")).toHaveTextContent("The harness was not created");
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("leaves a clean form without asking", async () => {
    renderAt("/harnesses/new");
    fireEvent.click(screen.getByTestId("harness-cancel"));
    expect(await screen.findByText("Harness list")).toBeInTheDocument();
  });

  it("asks before a dirty form is left, and Keep editing stays", async () => {
    const router = renderAt("/harnesses/new");
    type("harness-name", "made-here");
    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("harness-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/harnesses/new");
    expect(screen.getByTestId("harness-name")).toHaveValue("made-here");
  });

  it("says so when the harness to copy is not in the list", () => {
    renderAt("/harnesses/new?from=kagent/missing");
    expect(screen.getByTestId("harness-copy-not-found")).toBeInTheDocument();
    expect(screen.getByTestId("harness-name")).toHaveValue("");
  });
});

describe("harness edit form", () => {
  it("shows the adapter by its readable name and requires image and command for it", () => {
    mocks.harnesses = [harness("own", { byo: {}, workload: { image: IMAGE, command: ["/app/server"] } })];
    renderAt("/harnesses/kagent/own/edit");
    expect(screen.getByTestId("harness-adapter")).toHaveTextContent("Bring your own image");
    // Name, Namespace, Runtime adapter, Workload image, Command, Worker pool, Snapshot location.
    expect(screen.getAllByTestId("field-required")).toHaveLength(7);
    expect(screen.getAllByTestId("field-optional")).toHaveLength(1);
  });

  it("saves the change, shows the toast and lands on the list", async () => {
    mocks.harnesses = [
      harness("fast", { codex: {}, substrate: { workerPoolRef: { name: "gpu" }, snapshotPolicy: { location: "s3://x" } } }),
    ];
    mocks.update.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/harnesses/kagent/fast/edit");
    expect(screen.getByTestId("harness-name")).toBeDisabled();
    type("harness-image", IMAGE);
    fireEvent.click(screen.getByTestId("harness-submit"));

    expect(await screen.findByText("Harness list")).toBeInTheDocument();
    expect(mocks.update).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "fast",
      resource: {
        metadata: { namespace: "kagent", name: "fast", resourceVersion: "7" },
        spec: {
          codex: {},
          workload: { image: IMAGE },
          substrate: { workerPoolRef: { name: "gpu" }, snapshotPolicy: { location: "s3://x" } },
        },
      },
    });
    expect(mocks.success).toHaveBeenCalledWith("Saved harness fast");
  });
});
