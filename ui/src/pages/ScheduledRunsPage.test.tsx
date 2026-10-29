import { scheduleRouteID, scheduleRef, type ScheduledRun } from "@/api/domain/scheduledRuns";
import { wrap } from "@/api/grpc/wire";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it, vi } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { ScheduledRunService, ScheduledRunExecutionState } from "@/generated/kagent/api/v1/scheduled_runs_pb";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { ScheduledRunPage } from "./ScheduledRunsPage";
import { ScheduledRunEditPage } from "./ScheduledRunEditPage";

const schedule: ScheduledRun = { metadata: { name: "daily-report", namespace: "team", uid: "c686bd1d-9124-4e96-8df7-000000000001", resourceVersion: "123", generation: 1 }, spec: { agentRef: { name: "report" }, schedule: "0 9 * * *", prompt: "Report health", timeZone: "UTC", executionTimeout: "90.000123s" } };
const message = () => ({ ref: scheduleRef(schedule), resource: wrap("ScheduledRun", schedule) });

afterEach(() => {
  setApiTransport(undefined);
  vi.useRealTimers();
});

/*
 * Both routes, on every render: editing is an address now rather than a dialog, so a
 * save that leaves the form is a navigation and the detail page has to be there to
 * land on.
 */
function renderAt(entry: string) {
  render(<ThemeProvider theme={themeFor("dark")}>
    <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/schedules/:id" element={<ScheduledRunPage />} />
          <Route path="/schedules/:id/edit" element={<ScheduledRunEditPage />} />
        </Routes>
      </MemoryRouter>
    </SWRConfig>
  </ThemeProvider>);
}

const renderSchedule = () => renderAt(`/schedules/${scheduleRouteID(schedule)}`);
const renderEditor = () => renderAt(`/schedules/${scheduleRouteID(schedule)}/edit`);

it("recovers history after a new schedule finishes reconciling without a manual refresh", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  let reconciled = false;
  setApiTransport(createRouterTransport(({ service }) => service(ScheduledRunService, {
    getScheduledRun: () => ({ scheduledRun: {
      ref: scheduleRef(schedule),
      resource: wrap("ScheduledRun", {
        ...schedule,
        metadata: { ...schedule.metadata, resourceVersion: reconciled ? "124" : "123" },
        status: { observedGeneration: 1, conditions: [{ type: "Ready", status: reconciled ? "True" : "False" }] },
      }),
    } }),
    listScheduledRunExecutions: () => {
      if (!reconciled) throw new ConnectError("Scheduled run not found", Code.NotFound);
      return {};
    },
  })));
  renderSchedule();
  await screen.findByText("Could not load execution history");
  expect(screen.queryByText("No executions yet")).not.toBeInTheDocument();
  reconciled = true;
  await act(() => vi.advanceTimersByTimeAsync(10000));
  await screen.findByText("No executions yet");
  expect(screen.queryByText("Could not load execution history")).not.toBeInTheDocument();
});

it("reuses a manual run request ID after a lost response, then gives the next firing a new ID", async () => {
  const requestIds: string[] = [];
  setApiTransport(createRouterTransport(({ service }) => service(ScheduledRunService, {
    getScheduledRun: () => ({ scheduledRun: message() }),
    listScheduledRunExecutions: () => ({}),
    triggerScheduledRun: (request) => {
      expect(request.scheduledRunId).toBe(schedule.metadata.uid);
      requestIds.push(request.requestId);
      if (requestIds.length === 1) throw new ConnectError("Response lost", Code.Unavailable);
      return { execution: { id: "accepted", state: ScheduledRunExecutionState.PENDING } };
    },
  })));
  renderSchedule();
  const run = await screen.findByRole("button", { name: "Run" });
  fireEvent.click(run);
  await screen.findByText(/Response lost/);
  await waitFor(() => expect(run).toBeEnabled());
  fireEvent.click(run);
  await screen.findByText(/Execution queued/);
  await waitFor(() => expect(run).toBeEnabled());
  fireEvent.click(run);
  await waitFor(() => expect(requestIds).toHaveLength(3));
  expect(requestIds[0]).toBeTruthy();
  expect(requestIds[1]).toBe(requestIds[0]);
  expect(requestIds[2]).not.toBe(requestIds[0]);
  await waitFor(() => expect(run).toBeEnabled());
});

it("keeps unsaved edits after a conflict and preserves the exact timeout on the wire", async () => {
  let attempted = false;
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces: () => ({}) });
    service(ScheduledRunService, {
      getScheduledRun: () => ({ scheduledRun: message() }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: (request) => {
        expect(request.ref?.name).toBe(schedule.metadata.name);
        const resource = request.resource?.value as unknown as ScheduledRun;
        expect(resource.metadata.resourceVersion).toBe(schedule.metadata.resourceVersion);
        expect(resource.spec.executionTimeout).toBe(schedule.spec.executionTimeout);
        expect(resource.spec.prompt).toBe("Changed prompt");
        attempted = true;
        throw new ConnectError("Schedule changed. Reopen the editor.", Code.Aborted);
      },
    });
  }));
  renderEditor();
  const prompt = await screen.findByLabelText("Prompt", { exact: true });
  fireEvent.change(prompt, { target: { value: "Changed prompt" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await screen.findByText(/Schedule changed/);
  expect(attempted).toBe(true);
  // The form is still the one the reader typed into — a failed save must not remount
  // it, which is also what keeps the create request ID stable across a retry.
  expect(screen.getByLabelText("Prompt", { exact: true })).toHaveValue("Changed prompt");
});

it("serializes a fractional timeout without floating-point nanoseconds", async () => {
  let attempted = false;
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces: () => ({}) });
    service(ScheduledRunService, {
      getScheduledRun: () => ({ scheduledRun: message() }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: (request) => {
        expect((request.resource?.value as unknown as ScheduledRun).spec.executionTimeout).toBe("1.001s");
        attempted = true;
        return { scheduledRun: { ref: request.ref, resource: request.resource } };
      },
    });
  }));
  renderEditor();
  const timeout = await screen.findByLabelText("Execution timeout");
  expect(timeout).toHaveValue("90.000123s");
  fireEvent.change(timeout, { target: { value: "1.001s" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(attempted).toBe(true));
  // A saved edit leaves for the schedule it changed, so the form is gone and the
  // detail page's history is on screen.
  await screen.findByText("Execution history");
  expect(screen.queryByTestId("schedule-submit")).not.toBeInTheDocument();
});

it("requires explicit navigation before editing a recreated schedule", async () => {
  const replacement: ScheduledRun = { ...schedule, metadata: { ...schedule.metadata, uid: "c686bd1d-9124-4e96-8df7-000000000002", resourceVersion: "456" } };
  const updates: ScheduledRun[] = [];
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces: () => ({}) });
    service(ScheduledRunService, {
      getScheduledRun: () => ({ scheduledRun: { ref: scheduleRef(replacement), resource: wrap("ScheduledRun", replacement) } }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: request => {
        updates.push(request.resource?.value as unknown as ScheduledRun);
        return { scheduledRun: { ref: request.ref, resource: request.resource } };
      },
    });
  }));
  renderEditor();
  await screen.findByText("This schedule was recreated");
  expect(screen.queryByLabelText("Prompt", { exact: true })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save changes" })).not.toBeInTheDocument();
  expect(updates).toHaveLength(0);

  fireEvent.click(screen.getByRole("link", { name: "Edit current schedule" }));
  const prompt = await screen.findByLabelText("Prompt", { exact: true });
  fireEvent.change(prompt, { target: { value: "Explicit replacement edit" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(updates).toHaveLength(1));
  expect(updates[0].metadata.uid).toBe(replacement.metadata.uid);
  expect(updates[0].metadata.resourceVersion).toBe(replacement.metadata.resourceVersion);
  expect(updates[0].spec.prompt).toBe("Explicit replacement edit");
});


it("disables resource actions on the retained URL of a replaced schedule", async () => {
  const replacement: ScheduledRun = { ...schedule, metadata: { ...schedule.metadata, uid: "c686bd1d-9124-4e96-8df7-000000000002" } };
  const mutations = vi.fn();
  const getSchedule = vi.fn(() => ({ scheduledRun: { ref: scheduleRef(replacement), resource: wrap("ScheduledRun", replacement) } }));
  setApiTransport(createRouterTransport(({ service }) => service(ScheduledRunService, {
    getScheduledRun: getSchedule,
    listScheduledRunExecutions: () => ({}),
    deleteScheduledRun: mutations,
    updateScheduledRun: mutations,
    triggerScheduledRun: mutations,
  })));
  renderSchedule();
  await waitFor(() => expect(getSchedule).toHaveBeenCalled());
  await screen.findByText("No executions yet");
  for (const id of ["schedule-edit", "schedule-pause", "schedule-run"]) {
    const button = screen.getByTestId(id);
    expect(button).toBeDisabled();
    fireEvent.click(button);
  }
  const remove = screen.getByRole("button", { name: `Delete schedule ${schedule.metadata.name}` });
  expect(remove).toBeDisabled();
  fireEvent.click(remove);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(mutations).not.toHaveBeenCalled();
  expect(screen.queryByText(/deleted successfully/i)).not.toBeInTheDocument();
});

it("preserves the Agent reference when editing a schedule", async () => {
  let updated: ScheduledRun | undefined;
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces: () => ({}) });
    service(ScheduledRunService, {
      getScheduledRun: () => ({ scheduledRun: message() }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: request => {
        updated = request.resource?.value as unknown as ScheduledRun;
        return { scheduledRun: { ref: request.ref, resource: request.resource } };
      },
    });
  }));
  renderEditor();
  const prompt = await screen.findByTestId("schedule-prompt");
  fireEvent.change(prompt, { target: { value: "Inspect every incident." } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(updated).toBeDefined());
  expect(updated?.spec.agentRef).toEqual(schedule.spec.agentRef);
  expect(updated?.spec).not.toHaveProperty("agent");
  expect(updated?.spec.prompt).toBe("Inspect every incident.");
});

it("shows a schedule the controller rejected as not ready, with its reason", async () => {
  const rejected: ScheduledRun = { ...schedule, status: { observedGeneration: 1, conditions: [{ type: "Ready", status: "False", reason: "AgentNotFound", message: "Agent does not exist" }] } };
  setApiTransport(createRouterTransport(({ service }) => service(ScheduledRunService, {
    getScheduledRun: () => ({ scheduledRun: { ref: scheduleRef(rejected), resource: wrap("ScheduledRun", rejected) } }),
    listScheduledRunExecutions: () => ({}),
  })));
  renderSchedule();
  const meta = await screen.findByTestId("schedule-meta");
  expect(meta).toHaveTextContent("Not ready");
  expect(meta).not.toHaveTextContent("Active");
  expect(screen.getByTestId("schedule-not-ready-note")).toHaveTextContent("Agent does not exist");
});
