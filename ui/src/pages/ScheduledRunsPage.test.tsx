import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { ScheduledRunSchema, ScheduledRunService, ScheduledRunExecutionState } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
import { SystemService } from "@/generated/kagent/api/v1alpha1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { ScheduledRunPage } from "./ScheduledRunsPage";
import { ScheduledRunEditPage } from "./ScheduledRunEditPage";

const schedule = create(ScheduledRunSchema, {
  id: "c686bd1d-9124-4e96-8df7-000000000001", etag: "d686bd1d-9124-4e96-8df7-000000000001",
  config: { name: "Daily report", schedule: "0 9 * * *", prompt: "Report health", timeZone: "UTC", executionTimeout: { seconds: 90n, nanos: 123000 } },
});

afterEach(() => setApiTransport(undefined));

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

const renderSchedule = () => renderAt(`/schedules/${schedule.id}`);
const renderEditor = () => renderAt(`/schedules/${schedule.id}/edit`);

it("reuses a manual run request ID after a lost response, then gives the next firing a new ID", async () => {
  const requestIds: string[] = [];
  setApiTransport(createRouterTransport(({ service }) => service(ScheduledRunService, {
    getScheduledRun: () => ({ scheduledRun: schedule }),
    listScheduledRunExecutions: () => ({}),
    triggerScheduledRun: (request) => {
      expect(request.scheduledRunId).toBe(schedule.id);
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
      getScheduledRun: () => ({ scheduledRun: schedule }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: (request) => {
        expect(request.scheduledRunId).toBe(schedule.id);
        expect(request.etag).toBe(schedule.etag);
        expect(request.config?.executionTimeout).toEqual(schedule.config?.executionTimeout);
        expect(request.config?.prompt).toBe("Changed prompt");
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
      getScheduledRun: () => ({ scheduledRun: schedule }),
      listScheduledRunExecutions: () => ({}),
      updateScheduledRun: (request) => {
        expect(request.config?.executionTimeout?.seconds).toBe(1n);
        expect(request.config?.executionTimeout?.nanos).toBe(1000000);
        attempted = true;
        return { scheduledRun: { ...schedule, config: request.config } };
      },
    });
  }));
  renderEditor();
  const timeout = await screen.findByLabelText("Execution timeout (seconds)");
  expect(timeout).toHaveValue("90.000123");
  fireEvent.change(timeout, { target: { value: "1.001" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(attempted).toBe(true));
  // A saved edit leaves for the schedule it changed, so the form is gone and the
  // detail page's history is on screen.
  await screen.findByText("Execution history");
  expect(screen.queryByTestId("schedule-submit")).not.toBeInTheDocument();
});
