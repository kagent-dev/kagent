import { http, HttpResponse, delay } from "msw";
import type { Session } from "@/types";
import { Task } from "@a2a-js/sdk";

export {
  createMockSession,
  createTextPart,
  createDataPart,
  createMockMessage,
  createMockTextMessage,
  createMockTask,
  createMockToolCallTask,
} from "./factories";

/**
 * The backend URL that fetchApi constructs requests against.
 * In development / Storybook this resolves to localhost.
 */
const BACKEND_URL = "http://localhost:8083/api";

// ---------------------------------------------------------------------------
// Handler factories – compose these in per-story `beforeEach` calls
// ---------------------------------------------------------------------------

/** GET /sessions/:sessionId - returns a session with its read-only state. */
export function sessionWithEventsHandler(session: Session) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, () => {
    return HttpResponse.json({ data: { session, events: [], read_only: false } });
  });
}

/** GET /sessions/:sessionId – returns 404 */
export function sessionNotFoundHandler() {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, () => {
    return HttpResponse.json(
      { error: "Session not found" },
      { status: 404, headers: { "Content-Type": "application/json" } },
    );
  });
}

/** GET /sessions/:sessionId/tasks – returns wire JSON (same shape as the Go API). */
export function sessionTasksHandler(tasks: Task[]) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, () => {
    return HttpResponse.json({
      message: "Tasks fetched successfully",
      data: tasks.map((task) => Task.toJSON(task)),
    });
  });
}

/** GET /sessions/:sessionId/tasks – returns empty task list */
export function emptySessionTasksHandler() {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, () => {
    return HttpResponse.json({ message: "Tasks fetched successfully", data: [] });
  });
}

/** POST /sessions – creates a new session */
export function createSessionHandler(session: Session) {
  return http.post(`${BACKEND_URL}/sessions`, () => {
    return HttpResponse.json({ data: session });
  });
}

/** Adds an artificial delay to the session fetch (for loading-state stories). */
export function slowSessionWithEventsHandler(session: Session, ms = 2000) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, async () => {
    await delay(ms);
    return HttpResponse.json({ data: { session, events: [], read_only: false } });
  });
}

/** Adds an artificial delay to the tasks fetch */
export function slowSessionTasksHandler(tasks: unknown[], ms = 2000) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, async () => {
    await delay(ms);
    return HttpResponse.json({ message: "Tasks fetched successfully", data: tasks });
  });
}

/**
 * GET /mcp-apps/:namespace/:name/tools – returns the UI-capable tools (MCP Apps)
 * discovered for a server. Tools without `uiResourceUri` are filtered out by the UI.
 */
export function mcpAppToolsHandler(
  apps: Array<{ name: string; description?: string; uiResourceUri?: string }>,
  ms = 0,
) {
  return http.get(`${BACKEND_URL}/mcp-apps/:namespace/:name/tools`, async () => {
    if (ms > 0) {
      await delay(ms);
    }
    return HttpResponse.json({ message: "Tools fetched successfully", data: apps });
  });
}
