# Rewind Flow Design

## Goal

Allow the user to rewind a session to a point before a given invocation: show a "Rewind to here" (or "Undo to here") action per message in the UI, and when clicked, call the ADK Runner’s `rewind_async` so the session state and effective history are restored to that point. All requests (including the rewind) remain in the log.

## Invocation ID: Where It Comes From and Where It Goes

- **ADK**: Each `Event` has `invocation_id` (string). The Runner uses it for rewind and resume.
- **Python ADK → A2A**: When converting ADK events to A2A messages, we put `event.invocation_id` into message metadata as `kagent_invocation_id` (see `event_converter._get_context_metadata` and `ADKMetadata.kagent_invocation_id` in the UI).
- **UI**: Messages are built from task history (and streaming). Each message can have `metadata.kagent_invocation_id` when it came from an ADK event. So **the UI already has invocation_id on each message**; no new backend field is required to “detect” it.
- **Rewind request**: The UI must send the chosen **invocation_id** (and session id) to the backend so the Python Runner can call `runner.rewind_async(user_id, session_id, rewind_before_invocation_id)`.

So the only thing to “figure out” is how to **pass** that invocation_id from UI → backend → Runner. The design below does that by reusing the existing A2A message/stream path with a special “rewind” message.

---

## End-to-End Flow

```
┌─────────────┐     rewind request (invocation_id + session_id)      ┌─────────────┐     same A2A path      ┌─────────────────┐
│  UI         │ ──────────────────────────────────────────────────▶ │  Go backend │ ──────────────────────▶ │  Python ADK     │
│  (React)    │     POST /a2a/{ns}/{agent}  message/stream           │  (proxy)    │     POST /api/a2a/...  │  A2aAgentExecutor│
└─────────────┘     message.metadata.kagent_rewind_before_...      └─────────────┘                       └────────┬────────┘
       │                                                                                                              │
       │  "Rewind to here" on message with metadata.kagent_invocation_id                                            │
       │  → build message with contextId = sessionId, metadata.kagent_rewind_before_invocation_id = that id         │
       │                                                                                                              ▼
       │                                                                                                    execute() sees rewind
       │                                                                                                    → rewind_async(...)
       │                                                                                                    → enqueue "rewind done"
       │  SSE: task status (rewind complete)                                                                        │
       │◀─────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
       │  refresh session / task list so UI shows updated history
```

1. **UI**: User clicks "Rewind to here" on a message that has `metadata.kagent_invocation_id`.
2. **UI**: Sends a **rewind request** over the **same A2A path** as normal messages (no new Go route):
   - Method: `message/stream` (unchanged).
   - Params: a **control message** with:
     - `message.contextId` = current session id (so backend uses it as `context_id` → `session_id`).
     - `message.metadata.kagent_rewind_before_invocation_id` = the invocation id of the message they clicked (the one to rewind _before_).
     - Optional: `message.parts` = e.g. one empty or placeholder text part so the payload is still a valid message.
3. **Go backend**: No change. It keeps proxying `POST /api/a2a/...` to the Python agent. The request body is just a different message (rewind control message).
4. **Python \_agent_executor**: In `execute()` (or at the start of `_handle_request`):
   - Resolve `runner` and build `run_args` as today.
   - **Before** calling `runner.run_async(...)`, check if this is a rewind request:
     - e.g. `rewind_id = (context.message.metadata or {}).get("kagent_rewind_before_invocation_id")`.
   - If `rewind_id` is set:
     - Get `user_id` and `session_id` from `run_args` (same as today: `request.context_id` → session_id, `_get_user_id(request)` → user_id).
     - Call `await runner.rewind_async(user_id=..., session_id=..., rewind_before_invocation_id=rewind_id)`.
     - Enqueue a single **task status event** (e.g. “Rewind complete” or task completed) so the UI gets one SSE event and can refresh.
     - Return; do **not** call `runner.run_async(...)`.
   - Else: current behavior (prepare session, run_async, stream events).
5. **UI**: For the rewind request, the stream will contain a short SSE response (one or two events). On receiving the final event (e.g. task completed / rewind complete), refresh session/task list (e.g. refetch tasks for the session) so the conversation view updates to the rewound state.

No new HTTP endpoint is required; the same A2A `message/stream` carries both normal messages and the rewind control message. Invocation id flows: **ADK Event → A2A message metadata (kagent_invocation_id)** for display, and **UI → message.metadata.kagent_rewind_before_invocation_id** for the rewind request.

---

## Component-Level Design

### 1. UI (`ui/`)

- **ChatMessage (or message list)**
  - For each message, if `(message.metadata as ADKMetadata)?.kagent_invocation_id` is set, show a **"Rewind to here"** (or **"Undo to here"**) button.
  - **Important**: Use the **invocation_id of the message the user clicked** as `rewind_before_invocation_id`. That matches ADK semantics: “undo this invocation and everything after it.”

- **ChatInterface (or parent that has session + agent context)**
  - Add a handler, e.g. `handleRewindToMessage(message: Message)`:
    - Read `sessionId` (current chat session id).
    - Read `invocationId = (message.metadata as ADKMetadata)?.kagent_invocation_id`.
    - If either is missing, show a toast and return.
    - Call a new client method, e.g. `kagentA2AClient.sendRewindRequest(namespace, agentName, sessionId, invocationId)`.

- **Handle full Task in stream (Option B'):**
  - When handling the stream, if the event has `kind === 'task'` and `task.history` is present (e.g. after rewind), **replace** the conversation with `task.history`: call `setStoredMessages(task.history)` (or extract messages from `task.history` if the shape differs) and set chat status ready. Today the UI only does `setIsStreaming(true)` and returns for a Task; add a branch: if `task.history?.length`, replace messages and finalize. This way the same Task updates both the UI (immediate) and the task store (after save), and refetch/reload shows the rewound conversation.

- **a2aClient**
  - Add `sendRewindRequest(namespace, agentName, sessionId, rewindBeforeInvocationId)`:
    - Build the same JSON-RPC shape as `sendMessageStream` (method `message/stream`, params with a message).
    - Params:
      - `message`: role `"user"`, `contextId: sessionId`, `metadata: { kagent_rewind_before_invocation_id: rewindBeforeInvocationId }`, `parts: [{ kind: "text", text: "" }]` (or similar minimal part).
    - Use the same `fetch(proxyUrl, ...)` and same `processSSEStream` so the UI gets SSE events.
    - Return the same async iterable so the caller can consume the short stream and then refresh.

- **After rewind stream ends**
  - In the rewind handler, after consuming the stream (or on final event), **revert the UI** (see "Frontend: Revert the UI" below) and optionally refetch if needed.

- **Optional**: Disable "Rewind to here" on the **first** user message (nothing to rewind to before it), or show it only for assistant messages (so “rewind to before this response”).

## Frontend: Revert the UI and (Optional) Apply State Delta

### 1. Revert the UI — Yes

After rewind, the conversation view should show only the messages that are "before" the rewind point. The frontend should **revert the UI** so the user sees the rewound state immediately.

**Recommended: client-side revert**

- We already have `metadata.kagent_invocation_id` on each message and we know `rewind_before_invocation_id` (the invocation we rewound _before_).
- **Rule**: Show only messages that appear **before** the first message whose `invocation_id === rewind_before_invocation_id`. Hide that message and all messages after it (in display order).
- Implementation: After rewind completes, filter the current message list: find the index of the first message with `(message.metadata as ADKMetadata)?.kagent_invocation_id === rewindBeforeInvocationId`, then `setStoredMessages(prev => prev.slice(0, index))`. No refetch required for the revert itself.
- **Why client-side (until task store is updated):** Until the backend reflects rewound history (see "Making rewind persist" below), task history from the backend still contains all messages. So a simple refetch would return the unrewound view. Client-side truncation gives correct immediate feedback. **Once Option B is implemented**, refetch after rewind will return the updated task history and reload will show the rewound conversation.

**Refetch after revert**

- After reverting locally, refetch tasks (e.g. `getSessionTasks(sessionId)`). Once Option B is in place, the backend will return the updated (rewound) history and the UI will stay in sync; until then, client-side revert is the only way to show the rewound view, and refetch would restore the unrewound view. So: implement Option B so that refetch returns effective history; then rewind works on reload.

### 2. Apply the latest state delta — Optional

- **Server**: The rewind event's `state_delta` is already applied on the server when the Runner appends the rewind event: `session.state` is updated by the base `append_event` → `_update_session_state`. So the **session** is correct after rewind.
- **Frontend**: The UI today doesn't maintain a separate "session state" object; it mainly shows messages. So there's nothing to "apply" unless we expose session state to the client.
- **If we add session state to the UI later**: We could have the rewind-complete event include the new effective `session.state` (or a state_delta), and the frontend could apply it to local state (e.g. theme, form fields). For a minimal first version we can **skip** this and only revert the message list; add state sync later if needed.
- **Summary**: Reverting the UI (message list) is required. "Applying the latest state delta" on the client is optional and only relevant if the frontend keeps a copy of session state; for v1, reverting the UI is sufficient.

---

## Why rewind breaks on reload (task store vs session)

**What happens today:**

- **Session** (ADK session, KAgentSessionService): On rewind, the Runner appends a rewind event. Session state is updated correctly (state_delta applied). So the **session** is correct after rewind.
- **Task store** (what the UI uses): The UI loads the conversation via `getSessionTasks(sessionId)` → GET `/api/sessions/{id}/tasks`, which returns **tasks** and their **history** from the backend. Rewind does **not** update the task store: we only append a rewind event to the session. Task history is written when the agent runs (A2A flow saves task with history). So task history still contains all messages; it is never truncated or replaced on rewind.

**Result:** On reload (or refetch), the UI gets the same task history as before rewind. The conversation shows the unrewound version. So rewind can never really "work" unless we fix the source of truth for "what to show."

**Conclusion:** For rewind to work on reload, the **task store (or the API that returns conversation for the UI) must reflect the rewound state.** Client-side revert alone is lost on reload.

---

## Making rewind persist: two options

### Option A: Backend returns effective history when returning session tasks

- **Idea:** GET `/api/sessions/{session_id}/tasks` (or a dedicated "effective conversation" endpoint) computes **effective** history from session events instead of returning raw task history.
- **Flow:** Backend (Go): (1) Get session + events (already available via session API / DB). (2) Apply rewind logic: parse event data (ADK event JSON), find rewind events, determine effective event set (events before the rewind point). (3) Return something the UI can display: either tasks with history = effective messages (requires converting ADK events to A2A message shape in the backend), or a single "effective conversation" payload (e.g. list of messages).
- **Pros:** Single source of truth = session events; rewind is "just" a filter when reading. No need to rewrite task history on rewind.
- **Cons:** Backend must parse ADK event JSON and apply rewind logic; may need to convert ADK events to the message shape the UI expects (today that conversion lives in Python).

### Option B: On rewind, update the task store with effective history (recommended)

- **Idea:** After `runner.rewind_async()` succeeds, **update the task store** so that the conversation for this session = effective history (messages before the rewind point). Then GET `/api/sessions/{id}/tasks` keeps returning tasks as today, but their history is already rewound.
- **Flow:** Python executor, after `rewind_async()`: (1) Get session with events (`session_service.get_session(...)`). (2) Compute effective events: events before the first event with `invocation_id == rewind_before_invocation_id` (same logic as Runner). (3) Convert those ADK events to A2A messages (reuse existing event→message converter). (4) Call a new API to set effective history for the session, e.g. `PUT /api/sessions/{session_id}/effective_history` with body = list of messages (or update the relevant task(s) for this session with that history). Go backend: new handler that updates the task(s) for that session so their history = the provided list (e.g. one "conversation" task per session with merged history, or update all tasks for the session).
- **Pros:** Conversion stays in Python; UI and GET `/api/sessions/{id}/tasks` unchanged; reload shows correct rewound conversation.
- **Cons:** Need a new backend endpoint (e.g. PUT effective history) and a way to map "session" to "task(s)" to update (e.g. one task per session holding full conversation, or merge/update logic for multiple tasks per session).

**Recommendation:** Option B. After rewind, Python computes effective messages and pushes them to the backend via a new endpoint; the backend updates the task(s) for that session. Then the frontend can refetch after rewind and reload will show the rewound conversation. Client-side revert remains useful for immediate UI feedback; refetch then syncs with the updated task store.

### Option B': Send full Task with effective history (no new backend endpoint)

- **Idea:** Use the fact that the A2A streaming response can return a **full Task** (`SendStreamingMessageSuccessResponse.result` is `Task | Message | TaskStatusUpdateEvent | TaskArtifactUpdateEvent`). After rewind, build a **Task** with `history = effective_messages`, **save** it via the existing task store (`task_store.save(task)` → POST `/api/tasks`), and **enqueue** it so the frontend receives it in the stream. The same Task updates both the frontend (immediate UI) and the task store (reload).
- **Flow:**
  1. **Python executor** after `rewind_async()`:
     - Get session with events (`session_service.get_session(...)`).
     - Compute effective events (events before the first with `invocation_id == rewind_before_invocation_id`).
     - Convert those ADK events to A2A messages (reuse existing event→message conversion; need a minimal invocation context from session).
     - Build `Task(id=..., context_id=context.context_id, history=effective_messages, status=TaskStatus(state=TaskState.completed, ...))`.
     - **Save:** `await context.task_store.save(task)` (or pass task_store into executor). This does POST `/api/tasks` with the full task; the Go backend **already upserts** (`save` uses `OnConflict{UpdateAll: true}`), so the task is updated in the DB.
     - **Stream:** `await event_queue.enqueue_event(task)`. The event queue accepts Task (streaming result can be Task). The frontend receives this Task in the SSE stream.
  2. **Frontend:** When handling the stream, if the event has `kind === 'task'` and `task.history` is present, **replace** the conversation with `task.history`: e.g. `setStoredMessages(task.history)` (or extract messages from `task.history` if needed) and set chat status ready. Today the UI only does `setIsStreaming(true)` and returns for a Task; add a branch: if `task.history?.length`, replace messages and finalize.
  3. **Backend:** No new endpoint. POST `/api/tasks` already upserts. Use a **stable task_id per session** (e.g. `task_id = context_id = session_id`) so that the rewound task is the same task that GET `/api/sessions/{id}/tasks` returns for that session. Then on reload, the UI gets that task (with effective history) and displays it correctly.
- **Task ID for reload:** If each request currently gets a new `task_id`, then after rewind we'd have one new task (the rewind request's task) with effective history, plus older tasks with full history. `getSessionTasks` returns all; merging histories would duplicate or reorder wrong. So for Option B' to work on reload, use **one task per session**: e.g. `task_id = context_id` (session_id). When we rewind, we build `Task(id=context.context_id, context_id=context.context_id, history=effective_messages)` and save it (upsert). That requires the **client** to send the same `task_id` (e.g. session_id) for all messages in that session, or the **server** to assign `task_id = context_id` when building the request context, so that normal messages and rewind both update the same task. Then GET `/api/sessions/{id}/tasks` returns that one task (or that task first), and reload shows the correct conversation.
- **Summary:** Send a full **Task** (with effective history) in the stream and **save** it via existing `task_store.save(task)`. Frontend handles `kind === 'task'` with `task.history` by replacing the message list. Use a stable task_id per session so the saved task is the one returned on refetch; no new backend endpoint.

---

### 2. Python ADK (`python/packages/kagent-adk/`)

- **\_agent_executor.py**
  - At the start of `_handle_request` (or at the top of `execute` after building `run_args`):
    - Get `metadata = getattr(context.message, "metadata", None) or {}`.
    - `rewind_before_invocation_id = metadata.get("kagent_rewind_before_invocation_id")` (or the same key the UI uses).
    - If `rewind_before_invocation_id` is set:
      - `user_id = run_args["user_id"]`, `session_id = run_args["session_id"]`.
      - `await runner.rewind_async(user_id=user_id, session_id=session_id, rewind_before_invocation_id=rewind_before_invocation_id)`.
      - Enqueue a `TaskStatusUpdateEvent` (e.g. state completed, or a custom “rewind complete” message) with `context_id=context.context_id`, `final=True`.
      - Return (do not call `run_async`).
    - Otherwise, continue with existing logic (prepare session, append events, `run_async`, stream).

- **\_a2a.py**
  - No change required. Runner and session service are already created the same way; `rewind_async` is on the Runner and uses the same `KAgentSessionService` / `InMemorySessionService` as today.

- **Session service**
  - No change. Rewind is implemented by the Runner appending a rewind event via `append_event`; our session service already supports that.

### 3. Go backend (`go/`)

- **A2A proxy:** No change. A2A is already proxied to the Python agent; the rewind request is just another `message/stream` request with metadata `kagent_rewind_before_invocation_id`.
- **For rewind to persist on reload:**
  - **Option B' (no new endpoint):** Python saves a full `Task` (with effective history) via existing `task_store.save(task)` (POST `/api/tasks` upserts). No new Go endpoint; use stable `task_id = context_id` per session so the saved task is the one returned by GET `/api/sessions/{id}/tasks`.
  - **Option B (new endpoint):** Add e.g. `PUT /api/sessions/{session_id}/effective_history` that accepts a list of messages and updates the task(s) for that session so their history matches.

### 4. Invocation ID Consistency

- **Stored messages**: Already get `kagent_invocation_id` from ADK event metadata when events are converted to A2A messages and stored in task history. So when the UI loads messages from tasks, it already has `metadata.kagent_invocation_id` for each message that came from an ADK event.
- **Streaming messages**: The same converter attaches `kagent_invocation_id` to emitted messages, so streaming messages in the UI can also expose a rewind button once they have that metadata.

---

## Edge Cases and UX

- **Message without invocation_id**: Don’t show "Rewind to here" (e.g. old messages or system messages). Check `metadata.kagent_invocation_id` before showing the button.
- **First message**: Optionally hide rewind on the very first message (no “before” to rewind to).
- **Loading state**: While the rewind request is in flight, show a loading state and avoid duplicate rewind clicks.
- **Errors**: If `rewind_async` raises (e.g. invocation not found), executor catches it and enqueues a failed task status; UI shows the error like for any other failed request.

---

## Summary: Detecting and Passing Invocation ID

| Step              | Where                  | How                                                                                                                                       |
| ----------------- | ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| **Produce**       | ADK Runner / Event     | Every event has `event.invocation_id`.                                                                                                    |
| **Expose to A2A** | Python event_converter | Put in message metadata as `kagent_invocation_id`.                                                                                        |
| **Show in UI**    | ChatMessage            | Read `message.metadata.kagent_invocation_id`; show "Rewind to here" when present.                                                         |
| **Send rewind**   | UI → Backend           | Same A2A `message/stream`; message has `contextId` = session id and `metadata.kagent_rewind_before_invocation_id` = chosen invocation id. |
| **Handle rewind** | A2aAgentExecutor       | Read `context.message.metadata.kagent_rewind_before_invocation_id`; if set, call `runner.rewind_async(...)` and skip `run_async`.         |

No new routes or backend APIs are required; only a well-defined use of existing A2A message metadata and one branch in the executor to treat that message as a rewind control message.

---

## See also

- **[Task detection, display, and storage](.tasks-detection-and-storage.md)** — How the UI detects Task vs status/artifact events, when it replaces vs appends the conversation, and how tasks are stored and loaded via the backend APIs.
