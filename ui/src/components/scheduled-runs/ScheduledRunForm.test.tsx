import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ScheduledRunSchema } from "@/generated/kagent/api/v1/scheduled_runs_pb";
import { themeFor } from "@/theme/theme";
import { ScheduledRunNewPage } from "@/pages/ScheduledRunNewPage";
import { ScheduledRunEditPage } from "@/pages/ScheduledRunEditPage";
import { FIX_FIELDS_MESSAGE, FIX_FIELDS_TITLE } from "@/components/form/useSubmitValidation";

const mocks = vi.hoisted(() => ({ invoke: vi.fn(), toastSaved: vi.fn() }));

vi.mock("@/components/form/saveToast", () => ({ toastSaved: mocks.toastSaved }));
vi.mock("@/api/operations", () => ({ invoke: mocks.invoke }));
vi.mock("@/api", async (original) => ({
  ...(await original<typeof import("@/api")>()),
  useNamespaces: () => ({ data: [{ name: "kagent" }], isLoading: false, error: undefined }),
  useAgentsAcrossNamespaces: () => ({
    data: { agents: [{ ref: "kagent/helper", namespace: "kagent", name: "helper" }], refused: [] },
    isLoading: false,
    error: undefined,
  }),
  newConversationBlockedReason: () => undefined,
}));

const existing = create(ScheduledRunSchema, {
  id: "id-1",
  etag: "etag-1",
  config: { name: "Daily report", schedule: "0 9 * * *", prompt: "Report health", timeZone: "UTC", executionTimeout: { seconds: 90n } },
});

function renderAt(entry: string) {
  const router = createMemoryRouter(
    [
      { path: "/schedules", element: <p>Schedules list</p> },
      { path: "/schedules/new", element: <ScheduledRunNewPage /> },
      { path: "/schedules/:id", element: <p>Schedule detail</p> },
      { path: "/schedules/:id/edit", element: <ScheduledRunEditPage /> },
    ],
    { initialEntries: [entry] },
  );
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SWRConfig value={{ provider: () => new Map() }}>
        <RouterProvider router={router} />
      </SWRConfig>
    </ThemeProvider>,
  );
  return router;
}

const type = (testId: string, value: string) => fireEvent.change(screen.getByTestId(testId), { target: { value } });

/** Clicks an item in an open antd list, which renders each one twice. */
const pick = (name: string) =>
  fireEvent.click(screen.getAllByText(name).find((el) => el.classList.contains("ant-select-item-option-content"))!);

function chooseAgent() {
  fireEvent.mouseDown(within(screen.getByTestId("schedule-agent")).getByRole("combobox"));
  pick("kagent/helper");
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.invoke.mockImplementation(async (name: string, input: { config?: unknown }) => {
    if (name === "scheduledRuns.get") return { scheduledRun: existing };
    return { scheduledRun: create(ScheduledRunSchema, { id: "new-id", etag: "e", config: input.config as never }) };
  });
});

describe("new schedule page", () => {
  it("links back to the schedules list and marks every field", () => {
    renderAt("/schedules/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Schedules");
    expect(screen.getByLabelText(/^Schedule name/)).toHaveAccessibleName(/optional/);
    expect(screen.getByLabelText(/^Time zone/)).toHaveAccessibleName(/optional/);
    expect(screen.getByLabelText(/^Execution timeout/)).toHaveAccessibleName(/required/);
    // Agent, repeat, at time, prompt, timeout are required; name, time zone, enabled are optional.
    expect([screen.getAllByTestId("field-required").length, screen.getAllByTestId("field-optional").length]).toEqual([5, 3]);
    expect(screen.queryByText("Schedule Name")).not.toBeInTheDocument();
  });

  it("shows inline errors and a submit error on an invalid submit", () => {
    renderAt("/schedules/new");
    expect(screen.queryByTestId("schedule-form-error")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("schedule-submit"));
    expect(screen.getByText("Choose an agent.")).toBeInTheDocument();
    expect(screen.getByText("Enter a prompt.")).toBeInTheDocument();
    expect(screen.getByTestId("schedule-form-error")).toHaveTextContent(FIX_FIELDS_TITLE);
    expect(screen.getByTestId("schedule-form-error")).toHaveTextContent(FIX_FIELDS_MESSAGE);
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(mocks.invoke).not.toHaveBeenCalled();
  });

  it("asks for a day when the schedule is weekly", () => {
    renderAt("/schedules/new");
    fireEvent.mouseDown(within(screen.getByTestId("schedule-frequency")).getByRole("combobox"));
    pick("Weekly");
    fireEvent.click(screen.getByLabelText("Monday"));
    fireEvent.click(screen.getByTestId("schedule-submit"));
    expect(screen.getByText("Choose at least one day.")).toBeInTheDocument();
  });

  it("offers IANA zones in a searchable list and sends the chosen one", async () => {
    renderAt("/schedules/new");
    const zone = within(screen.getByTestId("schedule-timezone")).getByRole("combobox");
    fireEvent.change(zone, { target: { value: "Europe/Ber" } });
    expect(screen.getByRole("option", { name: "Europe/Berlin" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Asia/Tokyo" })).not.toBeInTheDocument();
    pick("Europe/Berlin");

    chooseAgent();
    type("schedule-prompt", "Check it.");
    fireEvent.click(screen.getByTestId("schedule-submit"));
    expect(await screen.findByText("Schedule detail")).toBeInTheDocument();
    const [name, input] = mocks.invoke.mock.calls[0];
    expect(name).toBe("scheduledRuns.create");
    expect(input).toMatchObject({
      agent: { namespace: "kagent", name: "helper" },
      config: { schedule: "0 9 * * *", timeZone: "Europe/Berlin", prompt: "Check it.", paused: false },
    });
  });

  it("submits on Enter in a single-line field, but not in the prompt or a list", async () => {
    renderAt("/schedules/new");
    chooseAgent();
    type("schedule-prompt", "Check it.");
    fireEvent.keyDown(screen.getByTestId("schedule-prompt"), { key: "Enter" });
    fireEvent.keyDown(within(screen.getByTestId("schedule-timezone")).getByRole("combobox"), { key: "Enter" });
    expect(mocks.invoke).not.toHaveBeenCalled();

    fireEvent.keyDown(screen.getByTestId("schedule-name"), { key: "Enter" });
    expect(await screen.findByText("Schedule detail")).toBeInTheDocument();
    expect(mocks.invoke).toHaveBeenCalledWith("scheduledRuns.create", expect.anything());
  });

  it("shows the save toast after a create", async () => {
    renderAt("/schedules/new");
    chooseAgent();
    type("schedule-name", "Nightly");
    type("schedule-prompt", "Check it.");
    fireEvent.click(screen.getByTestId("schedule-submit"));
    await screen.findByText("Schedule detail");
    expect(mocks.toastSaved).toHaveBeenCalledWith("created", "schedule", "Nightly");
  });

  it("guards the form while the create is pending, then lands on the schedule", async () => {
    let finish!: () => void;
    mocks.invoke.mockImplementation((_: string, input: { config?: unknown }) => new Promise((resolve) => {
      finish = () => resolve({ scheduledRun: create(ScheduledRunSchema, { id: "new-id", etag: "e", config: input.config as never }) });
    }));
    const router = renderAt("/schedules/new");
    chooseAgent();
    type("schedule-prompt", "Check it.");
    fireEvent.click(screen.getByTestId("schedule-submit"));
    await waitFor(() => expect(mocks.invoke).toHaveBeenCalledWith("scheduledRuns.create", expect.anything()));

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("schedule-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/schedules/new");

    finish();
    expect(await screen.findByText("Schedule detail")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps the form and explains a failed create", async () => {
    mocks.invoke.mockRejectedValue(new Error("boom"));
    renderAt("/schedules/new");
    chooseAgent();
    type("schedule-prompt", "Check it.");
    fireEvent.click(screen.getByTestId("schedule-submit"));
    expect(await screen.findByTestId("schedule-form-error")).toHaveTextContent("boom");
    expect(mocks.toastSaved).not.toHaveBeenCalled();
  });

  it("cancels straight to the list when clean, and asks first when dirty", async () => {
    const router = renderAt("/schedules/new");
    type("schedule-prompt", "Draft");
    fireEvent.click(screen.getByTestId("schedule-cancel"));
    expect(await screen.findByTestId("schedule-discard-body")).toHaveTextContent("Leaving discards your edits.");
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/schedules/new");
    fireEvent.click(screen.getByTestId("schedule-cancel"));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Schedules list")).toBeInTheDocument();
  });

  it("leaves a clean form without asking", async () => {
    renderAt("/schedules/new");
    fireEvent.click(screen.getByTestId("schedule-cancel"));
    expect(await screen.findByText("Schedules list")).toBeInTheDocument();
  });
});

describe("edit schedule page", () => {
  it("saves, shows the toast and returns to the schedule", async () => {
    renderAt("/schedules/id-1/edit");
    const prompt = await screen.findByLabelText(/^Prompt/);
    fireEvent.change(prompt, { target: { value: "New prompt" } });
    fireEvent.click(screen.getByTestId("schedule-submit"));
    expect(await screen.findByText("Schedule detail")).toBeInTheDocument();
    expect(mocks.toastSaved).toHaveBeenCalledWith("saved", "schedule", "Daily report");
    expect(mocks.invoke).toHaveBeenCalledWith("scheduledRuns.update", expect.objectContaining({ etag: "etag-1" }));
  });
});
