# Task Detection, Display, and Storage

This document explains how **tasks** are detected and displayed in the UI, and how they are stored and retrieved on the backend. It ties together the A2A stream event types, the frontend message-handling logic, and the Kagent task/session APIs.

---

## 1. What is a “Task” in this context?

In the A2A (Agent-to-Agent) protocol and Kagent:

- A **Task** is an A2A type that represents a unit of work: it has an `id`, `contextId` (session id), `history` (list of messages), `status`, and optional `metadata`/`artifacts`.
- The **stream** can deliver different event types: full **Task**, **TaskStatusUpdateEvent**, **TaskArtifactUpdateEvent**, or **Message**.
- The **UI** shows a single conversation as a merged view of:
  - **Stored messages**: from tasks loaded via `GET /api/sessions/{session_id}/tasks` (and from a full **Task** event on rewind).
  - **Streaming messages**: from status/artifact updates during an active run.

So “detecting and displaying tasks” means: (1) recognizing what kind of event we got from the stream or API, (2) deciding whether to **replace** the conversation (full Task) or **append/update** (status/artifact), and (3) where that data is persisted (task store).

---

## 2. Where task data comes from

### 2.1 Initial load (session open)

| Source        | API                                    | What the UI does                                                                                                                            |
| ------------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Session tasks | `GET /api/sessions/{session_id}/tasks` | Returns an array of **Task** objects. UI calls `extractMessagesFromTasks(tasks)` to turn them into a flat list of **Message**s for display. |

- **Backend**: Go server → `HandleListTasksForSession` → `DatabaseService.ListTasksForSession(sessionID)` → returns all tasks whose `session_id` (stored as `ContextID` in the Task) matches.
- **UI**: `getSessionTasks(sessionId)` in `app/actions/sessions.ts` calls the API; `extractMessagesFromTasks(tasks)` in `messageHandlers.ts` iterates each task’s `task.history`, keeps items with `kind === "message"`, and deduplicates by `messageId`. Result is one array of messages shown as the conversation.

So on load, “tasks” = whatever is stored in the DB for that session. There can be **multiple tasks per session** (e.g. one per run or one per rewind); the UI **merges** their histories and dedupes by `messageId`.

### 2.2 Live stream (sending a message or rewind)

The UI uses the same A2A `message/stream` path for:

- **Normal send**: user message → agent runs → stream of status/artifact events (and optionally a final Task).
- **Rewind only**: rewind request → backend runs rewind → stream sends **one full Task** with effective history.
- **Rewind + invoke**: rewind request with a new message → backend rewinds then runs → stream sends **one full Task** (history) then status/artifact events for the new turn.

Events in the stream are JSON-RPC results; the client uses `eventData.result || eventData` as the payload. So the “message” passed to the frontend handler can be a Task, a TaskStatusUpdateEvent, a TaskArtifactUpdateEvent, or a Message.

---

## 3. Detecting event type (task vs status vs artifact vs message)

Detection lives in **`ui/src/lib/utils.ts`** (`messageUtils`) and is used in **`ui/src/lib/messageHandlers.ts`** in `handleMessageEvent`.

### 3.1 Full Task (replace conversation)

Used when the backend sends a **complete Task** (e.g. after rewind) that should **replace** the current conversation, not append.

- **`messageUtils.isA2ATask(message)`**
  - `true` when `message.kind === "task"` (canonical A2A shape).

- **`messageUtils.isTaskLike(message)`**
  - Fallback when the stream doesn’t send `kind` (e.g. different serialization).
  - `true` when:
    - `message.history` is a non-empty array, and
    - `message.id` or `message.contextId` is a string.

If **either** is true, the handler treats the payload as a Task. If `task.history` has length > 0, it **replaces** stored messages with `task.history` and clears streaming state (so we don’t append the same history again and avoid “ABCA”). See [Rewind flow](./rewind-flow.md) for why rewind sends a full Task.

### 3.2 Status update (append/update streaming state)

- **`messageUtils.isA2ATaskStatusUpdate(message)`**
  - `true` when `message.kind === "status-update"`.

These events carry incremental status (e.g. working, completed) and often a **message** (e.g. assistant text). The handler appends or updates **streaming** messages and streaming content; it does **not** replace the whole conversation.

### 3.3 Artifact update (append tool/artifact content)

- **`messageUtils.isA2ATaskArtifactUpdate(message)`**
  - `true` when `message.kind === "artifact-update"`.

Used for tool calls/artifacts. The handler converts them to display messages and **appends** to the streaming list.

### 3.4 Plain message

- **`messageUtils.isA2AMessage(message)`**
  - `true` when `message.kind === "message"`.

Treated as a single message and appended (e.g. via `handleOtherMessage`).

### 3.5 Unknown

If none of the above match, the payload is logged and passed to `handleOtherMessage` (append). So any unrecognized shape (e.g. a Task missing `kind`) would previously have been appended and could duplicate history; **`isTaskLike`** is there to avoid that.

---

## 4. Display logic (replace vs append)

In **`createMessageHandlers`** (`messageHandlers.ts`), **`handleMessageEvent`** drives what the user sees:

| Event type          | Condition                                                                    | Action                                                                                                                      |
| ------------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| **Full Task**       | `isA2ATask(message) \|\| isTaskLike(message)`, and `task.history.length > 0` | **Replace**: `setStoredMessages(task.history)`, `setMessages(() => [])`, clear streaming content and set chat status ready. |
| **Full Task**       | Same, but `task.history` empty                                               | `setIsStreaming(true)` (e.g. start of stream).                                                                              |
| **Status update**   | `isA2ATaskStatusUpdate(message)`                                             | Append or update streaming messages/content; on final status, finalize streaming.                                           |
| **Artifact update** | `isA2ATaskArtifactUpdate(message)`                                           | Convert to messages and append to streaming list.                                                                           |
| **Message**         | `isA2AMessage(message)`                                                      | Append via `handleA2AMessage`.                                                                                              |
| **Other**           | —                                                                            | Append via `handleOtherMessage`.                                                                                            |

So:

- **Replace** happens only for a **full Task with non-empty history** (e.g. rewind response). That keeps the conversation in sync with the backend’s “effective history” and avoids appending the same history again (no “ABCA”).
- **Append** happens for status/artifact events and plain messages during a run.

The UI also does **optimistic rewind**: when the user clicks “Rewind to here”, the client immediately truncates the displayed list to the rewind point (using `metadata.kagent_invocation_id`), then when the Task event arrives it replaces again with `task.history` (server authority).

---

## 5. How tasks are stored (backend)

### 5.1 Storage model (Go)

- **Table**: Tasks are stored in the DB with at least: `id` (task id), `session_id` (from Task’s `ContextID`), `data` (JSON-serialized A2A Task).
- **APIs**:
  - **POST /api/tasks** — create/update a task (body = A2A Task). Handler uses `DatabaseService.StoreTask`; the DB layer uses an upsert (e.g. `OnConflict`), so the same task id updates the same row.
  - **GET /api/tasks/{task_id}** — return one task.
  - **GET /api/sessions/{session_id}/tasks** — return all tasks for that session (used by the UI for initial load).
  - **DELETE /api/tasks/{task_id}** — delete a task.

So “tasks” in storage = rows keyed by task `id`, with `session_id` used for listing by session.

### 5.2 Who writes tasks (Python ADK)

- **Rewind**: In **`_agent_executor._handle_rewind`**, after computing effective history the executor builds a **Task** with `id = context_id = session_id` (one task per session for rewind), then:
  - **Saves**: `await self._task_store.save(task)` → **POST /api/tasks** (upsert).
  - **Streams**: `await event_queue.enqueue_event(task)` → frontend receives that Task and replaces the conversation (see above).

So after rewind, the task store holds the **effective history** for that session; reload/fetch uses that same task.

- **Normal runs**: The A2A server may also save tasks at the end of a run (depending on the a2a package). If so, multiple tasks per session can exist (e.g. one per run); the UI merges and dedupes by `messageId` when loading.

### 5.3 KAgent task store (Python → Go)

- **KAgentTaskStore** (in `kagent-core`): implements A2A `TaskStore`; `save(task)` POSTs the task to **/api/tasks** (JSON body = `task.model_dump(mode="json")`). Partial/streaming events can be stripped from `task.history` before save so only final messages are persisted.

So “how tasks are stored” = **POST /api/tasks** (upsert by task id), and **GET /api/sessions/{id}/tasks** for listing. The UI then turns that list of tasks into a single conversation view via `extractMessagesFromTasks`.

---

## 6. End-to-end summary

| Question                                                            | Answer                                                                                                                                                                                                            |
| ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **How does the UI know if an event is a full Task?**                | `messageUtils.isA2ATask(message)` (kind === "task") or `messageUtils.isTaskLike(message)` (history array + id/contextId).                                                                                         |
| **When does the UI replace the conversation instead of appending?** | Only when handling a **full Task** with non-empty `task.history` (e.g. rewind). Then it sets stored messages to `task.history` and clears streaming state.                                                        |
| **Where do “stored” messages come from on load?**                   | `GET /api/sessions/{session_id}/tasks` → `extractMessagesFromTasks(tasks)` → flat, deduped list of messages from all tasks’ `history`.                                                                            |
| **Where do tasks get written?**                                     | Python ADK calls `task_store.save(task)` (e.g. after rewind) → **POST /api/tasks**; Go DB upserts by task id. Rewind uses `task_id = context_id = session_id` so one task per session reflects effective history. |
| **Why is “task-like” detection needed?**                            | So that even if the stream omits `kind: "task"`, we still recognize a payload with `history` + id/contextId as a Task and **replace** instead of appending, avoiding duplicate or wrong history (e.g. ABCA).      |

For rewind-specific flow (invocation id, rewind request shape, rewind+invoke), see [Rewind flow](./rewind-flow.md).
